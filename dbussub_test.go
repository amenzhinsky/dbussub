package dbussub

import (
	"io"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestSubscribe(t *testing.T) {
	conn := connect(t, "org.dbussub")
	defer checkClose(t, conn)

	mgr := NewManager(connect(t, ""))
	defer checkClose(t, mgr)

	s0, err := mgr.Subscribe(
		WithSender("org.dbussub"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, s0)

	s1, err := mgr.Subscribe(
		WithSender("org.dbussub"),
		WithPath("/org/dbussub"),
		WithInterface("org.dbussub.Sig"),
		WithMember("Int"),
		WithSendChannel(make(chan *dbus.Signal, 0)),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, s1)

	// invalid path
	emit(t, conn, "/", "org.dbussub.Sig.Int", 1)
	checkSignal(t, s0, 1)
	checkNoSignal(t, s1)

	// invalid interface
	emit(t, conn, "/org/dbussub", "org.freedesktop.systemd1.Int", 2)
	checkSignal(t, s0, 2)
	checkNoSignal(t, s1)

	// invalid member
	emit(t, conn, "/org/dbussub", "org.dbussub.Sig.Str", 3)
	checkSignal(t, s0, 3)
	checkNoSignal(t, s1)

	// valid path
	emit(t, conn, "/org/dbussub", "org.dbussub.Sig.Int", 4)
	checkSignal(t, s0, 4)
	checkSignal(t, s1, 4)

	conn1 := connect(t, "")
	defer checkClose(t, conn1)

	// invalid sender
	emit(t, conn1, "/org/dbussub", "org.dbussub.Sig.Int", 5)
	checkNoSignal(t, s0)
	checkNoSignal(t, s1)
}

func TestPathNamespace(t *testing.T) {
	conn := connect(t, "org.dbussub")
	defer checkClose(t, conn)

	mgr := NewManager(connect(t, ""))
	defer checkClose(t, mgr)

	sub, err := mgr.Subscribe(
		WithSender("org.dbussub"),
		WithPathNamespace("/org/dbussub"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer checkClose(t, sub)

	emit(t, conn, "/", "org.dbussub.Sig.Int", 1)
	checkNoSignal(t, sub)

	emit(t, conn, "/org", "org.dbussub.Sig.Int", 2)
	checkNoSignal(t, sub)

	emit(t, conn, "/org/dbussub", "org.dbussub.Sig.Int", 3)
	checkSignal(t, sub, 3)

	emit(t, conn, "/org/dbussub/child", "org.dbussub.Sig.Int", 4)
	checkSignal(t, sub, 4)
}

func TestClose(t *testing.T) {
	mgr := NewManager(connect(t, ""))
	sub, err := mgr.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	if err = mgr.Close(); err != nil {
		t.Fatal(err)
	}

	if err = mgr.Err(); err != errClosed {
		t.Errorf("mgr.Err() = %v, want %v", err, errClosed)
	}
	if err = sub.Err(); err != errClosed {
		t.Errorf("sub.Err() = %v, want %v", err, errClosed)
	}
}

func checkClose(t *testing.T, closer io.Closer) {
	if err := closer.Close(); err != nil {
		t.Fatalf("close error: %s", err)
	}
}

func checkSignal(t *testing.T, sub *Subscription, want int32) {
	t.Helper()
	for {
		select {
		case sig, ok := <-sub.C():
			if !ok {
				t.Fatal("subscription is closed")
			}
			if have := sig.Body[0].(int32); have != want {
				t.Fatalf("received #%d signal, want #%d", have, want)
			}
			return
		case <-time.After(time.Second):
			t.Fatal("subscription timed out")
		}
	}
}

func checkNoSignal(t *testing.T, sub *Subscription) {
	t.Helper()
	select {
	case sig, ok := <-sub.C():
		if !ok {
			t.Fatal("subscription is closed")
		}
		t.Fatalf("received #%d signal but shouldn't", sig.Body[0].(int32))
	case <-time.After(5 * time.Millisecond):
	}
}

func emit(t *testing.T, conn *dbus.Conn, path dbus.ObjectPath, name string, i int32) {
	t.Helper()
	if err := conn.Emit(path, name, i); err != nil {
		t.Fatal(err)
	}
}

func connect(t *testing.T, dest string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	if err = conn.Auth(nil); err != nil {
		t.Fatal(err)
	}
	if err = conn.Hello(); err != nil {
		t.Fatal(err)
	}

	if dest != "" {
		reply, err := conn.RequestName(dest, dbus.NameFlagDoNotQueue)
		if err != nil {
			t.Fatal(err)
		}
		if reply != dbus.RequestNameReplyPrimaryOwner {
			t.Fatalf("unexpected RequestName reply: %d", reply)
		}
	}
	return conn
}
