# dbussub

D-Bus signal subscription routines.

## Usage

```go
package main

import (
	"fmt"
	"os"

	"github.com/amenzhinsky/dbussub"
	"github.com/godbus/dbus/v5"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := dbus.SystemBus()
	if err != nil {
		return err
	}
	defer conn.Close()

	// subscribe to specific destination and interface
	mgr := dbussub.NewManager(conn)
	sub1, err := mgr.Subscribe(
		dbussub.WithSender("org.freedesktop.systemd1"),
		dbussub.WithInterface("org.freedesktop.systemd1.Manager"),
	)
	if err != nil {
		return err
	}
	defer mgr.Unsubscribe(sub1)

	go printSignals(sub1)

	// subscribe to all signals
	sub2, err := mgr.Subscribe()
	if err != nil {
		return err
	}
	defer mgr.Unsubscribe(sub2)

	go printSignals(sub2)

	select {}
	return nil
}

func printSignals(sub *dbussub.Subscription) {
	for ev := range sub.C() {
		fmt.Printf("%s: %v\n", ev.Name, ev.Body)
	}
}
```
