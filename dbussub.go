package dbussub

import (
	"errors"
	"strings"
	"sync"

	"github.com/godbus/dbus"
)

// NewManager allocates and returns a new manager object.
func NewManager(conn *dbus.Conn) *Manager {
	if conn == nil {
		panic("conn is nil")
	}
	mgr := &Manager{
		conn: conn,
		done: make(chan struct{}),
	}
	// TODO: subscribe to NameLost signals to track uniq names
	go mgr.rx()
	return mgr
}

// Manager is a subscriptions manager.
type Manager struct {
	mu   sync.RWMutex
	conn *dbus.Conn
	subs []*Subscription
	done chan struct{}
	err  error // TODO
}

func (mgr *Manager) rx() {
	sigc := make(chan *dbus.Signal, 32)
	mgr.conn.Signal(sigc)
	defer mgr.conn.RemoveSignal(sigc)

	for {
		select {
		case sig, ok := <-sigc:
			if !ok {
				mgr.Close()
				return
			}
			mgr.mu.RLock()
			for _, sub := range mgr.subs {
				if sub.matches(sig) {
					// try to send the signal first, if it fails wrap it with
					// a goroutine to avoid potential blocking of the loop
					select {
					case sub.send <- sig:
					case <-sub.done:
					default:
						go func() {
							select {
							case sub.send <- sig:
							case <-sub.done:
							}
						}()
					}
				}
			}
			mgr.mu.RUnlock()
		case <-mgr.done:
			return
		}
	}
}

func (mgr *Manager) addMatch(sub *Subscription) error {
	return mgr.conn.BusObject().Call(
		"org.freedesktop.DBus.AddMatch", 0, sub.rule(),
	).Store()
}

func (mgr *Manager) removeMatch(sub *Subscription) error {
	return mgr.conn.BusObject().Call(
		"org.freedesktop.DBus.RemoveMatch", 0, sub.rule(),
	).Store()
}

func (mgr *Manager) getNameOwner(name string) (string, error) {
	var s string
	if err := mgr.conn.BusObject().Call(
		"org.freedesktop.DBus.GetNameOwner", 0, name,
	).Store(&s); err != nil {
		return "", err
	}
	return s, nil
}

// Subscribe creates a new subscription with the given options.
//
// For more information about options see match rules that correspond to
// `Option` functions [here](https://dbus.freedesktop.org/doc/dbus-specification.html#message-bus-routing-match-rules).
func (mgr *Manager) Subscribe(opts ...Option) (*Subscription, error) {
	sub := &Subscription{
		mgr:  mgr,
		done: make(chan struct{}),
		send: make(chan *dbus.Signal, 1),
	}
	for _, opt := range opts {
		opt(sub)
	}
	if sub.sender != "" {
		uniq, err := mgr.getNameOwner(sub.sender)
		if err != nil {
			return nil, err
		}
		sub.senderUniq = uniq
	}
	mgr.mu.Lock()
	if err := mgr.addMatch(sub); err != nil {
		mgr.mu.Unlock()
		return nil, err
	}
	mgr.subs = append(mgr.subs, sub)
	mgr.mu.Unlock()
	return sub, nil
}

func (mgr *Manager) unsubscribe(sub *Subscription) error {
	mgr.mu.Lock()
	for i := range mgr.subs {
		if sub != mgr.subs[i] {
			continue
		}
		if err := mgr.removeMatch(sub); err != nil {
			mgr.mu.Unlock()
			return err
		}
		mgr.subs[i].close()
		mgr.subs = append(mgr.subs[:i], mgr.subs[i+1:]...)
		mgr.mu.Unlock()
		return nil
	}
	mgr.mu.Unlock()
	return errors.New("not found")
}

// Done returns the channel that is closed when the manager is closed or encountered an error.
func (mgr *Manager) Done() <-chan struct{} {
	return mgr.done
}

// Err returns the reason error why the manager has been closed.
//
// It always returns a non-nil value and it's supposed to be called only after the channel
// returned by `Done` method is closed.
func (mgr *Manager) Err() error {
	return mgr.err
}

// Close closes the manager, all its subs and D-Bus connection.
func (mgr *Manager) Close() error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	select {
	case <-mgr.done:
		return nil
	default:
	}
	close(mgr.done)
	for _, s := range mgr.subs {
		s.close()
	}
	return mgr.conn.Close()
}

// Option is a subscription option.
type Option func(s *Subscription)

// WithSender matches messages sent by the named sender,
// it can be both a unique or a well-known name.
func WithSender(sender string) Option {
	return func(s *Subscription) {
		// we should distinguish unique and well-known names,
		// because even if we subscribe to a well-known one, we may
		// receive signals with a unique name in the Name field.
		if sender[0] == ':' {
			s.senderUniq = sender
		} else {
			s.sender = sender
		}
	}
}

// WithInterface matches messages sent by the named interface.
func WithInterface(iface string) Option {
	return func(s *Subscription) {
		s.iface = iface
	}
}

// WithMember matches messages that have the signal name.
func WithMember(member string) Option {
	return func(s *Subscription) {
		s.member = member
	}
}

// WithPath matches messages that sent by the named object path.
//
// Cannot be combined with `WithPath`.
func WithPath(path dbus.ObjectPath) Option {
	return func(s *Subscription) {
		s.path = path
	}
}

// WithPathNamespace matches messages that's path matches the named namespace.
//
// Cannot be combined with `WithPath`.
func WithPathNamespace(namespace dbus.ObjectPath) Option {
	return func(s *Subscription) {
		s.pathNamespace = namespace
	}
}

// WithSendChannel sets the channel all matching signals are sent to,
// useful to make use of the channel buffering, by default it's 1.
func WithSendChannel(send chan *dbus.Signal) Option {
	return func(s *Subscription) {
		s.send = send
	}
}

// Subscription represents a receiver of D-Bus signals.
type Subscription struct {
	mgr  *Manager
	send chan *dbus.Signal
	done chan struct{}

	sender        string
	senderUniq    string
	iface         string
	member        string
	path          dbus.ObjectPath
	pathNamespace dbus.ObjectPath
}

// C returns the channel where all incoming signals are sent to.
func (sub *Subscription) C() <-chan *dbus.Signal {
	return sub.send
}

func (sub *Subscription) rule() string {
	b := strings.Builder{}
	b.WriteString("type='signal'")
	if sub.sender != "" {
		b.WriteString(",sender='" + sub.sender + "'")
	} else if sub.senderUniq != "" {
		b.WriteString(",sender='" + sub.senderUniq + "'")
	}
	if sub.iface != "" {
		b.WriteString(",interface='" + sub.iface + "'")
	}
	if sub.member != "" {
		b.WriteString(",member='" + sub.member + "'")
	}
	if sub.path != "" {
		b.WriteString(",path='" + string(sub.path) + "'")
	}
	if sub.pathNamespace != "" {
		b.WriteString(",path_namespace='" + string(sub.pathNamespace) + "'")
	}
	return b.String()
}

func (sub *Subscription) matches(sig *dbus.Signal) bool {
	if sub.path != "" && sub.path != sig.Path {
		return false
	}
	if sig.Sender[0] == ':' {
		if sub.senderUniq != "" && sub.senderUniq != sig.Sender {
			return false
		}
	} else {
		if sub.sender != "" && sub.sender != sig.Sender {
			return false
		}
	}
	// TODO: optimize to reduce allocations number
	i := strings.LastIndex(sig.Name, ".")
	iface, member := sig.Name[:i], sig.Name[i+1:]
	if sub.iface != "" && sub.iface != iface {
		return false
	}
	if sub.member != "" && sub.member != member {
		return false
	}
	if sub.pathNamespace != "" && !strings.HasPrefix(string(sig.Path), string(sub.pathNamespace)) {
		return false
	}
	return true
}

func (sub *Subscription) close() {
	close(sub.done)
	close(sub.send)
}

// Close closes the subscription and the channel returned by `C()`.
func (sub *Subscription) Close() error {
	return sub.mgr.unsubscribe(sub)
}
