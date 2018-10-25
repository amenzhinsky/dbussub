# dbussub

D-Bus signal subscription routines.

## Usage

```go
package main

import (
	"fmt"
	"os"

	"github.com/amenzhinsky/dbus-codegen-go"
	"github.com/godbus/dbus"
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
	sub, err := mgr.Subscribe(
		dbussub.WithSender("org.freedesktop.systemd1"),
		dbussub.WithInterface("org.freedesktop.systemd1.Manager"),
	)
	if err != nil {
		return err
	}
	go printSignals(sub)

	// subscribe to all signals
	sub, err = mgr.Subscribe()
	if err != nil {
		return err
	}
	go printSignals(sub)

	select {}
	return nil
}

func printSignals(sub *dbussub.Subscription) {
	for ev := range sub.C() {
		fmt.Printf("%s: %v\n", ev.Name, ev.Body)
	}
}
```
