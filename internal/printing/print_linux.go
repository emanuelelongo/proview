package printing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	portalService    = "org.freedesktop.portal.Desktop"
	portalPath       = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	printInterface   = "org.freedesktop.portal.Print"
	requestInterface = "org.freedesktop.portal.Request"
)

func printNative(ctx context.Context, job Job, path string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("desktop printing service unavailable: %w", err)
	}
	defer conn.Close()
	// PreparePrint supplies the desktop dialog without a GTK dependency. The
	// returned token submits the PDF using those settings without a second dialog.
	settings := map[string]dbus.Variant{
		"print-pages":        dbus.MakeVariant("all"),
		"cups-print-scaling": dbus.MakeVariant("fit"),
		"cups-fit-to-page":   dbus.MakeVariant("true"),
	}
	result, err := portalRequest(ctx, conn, "PreparePrint", job.ParentID, job.Title,
		settings, map[string]dbus.Variant{}, map[string]dbus.Variant{})
	if err != nil {
		return err
	}
	token, ok := result["token"].Value().(uint32)
	if !ok {
		return fmt.Errorf("desktop printing service returned no print settings")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = portalRequest(ctx, conn, "Print", job.ParentID, job.Title, dbus.UnixFD(file.Fd()),
		map[string]dbus.Variant{"token": dbus.MakeVariant(token)})
	return err
}

func portalRequest(ctx context.Context, conn *dbus.Conn, method string, args ...any) (map[string]dbus.Variant, error) {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	options := args[len(args)-1].(map[string]dbus.Variant)
	options["handle_token"] = dbus.MakeVariant("proview_" + hex.EncodeToString(nonce[:]))
	options["modal"] = dbus.MakeVariant(true)
	options["supported_output_file_formats"] = dbus.MakeVariant([]string{"pdf"})
	// Subscribe before calling: a portal may emit Response before the method
	// returns its request handle. Filter the queued signals by that handle below.
	match := []dbus.MatchOption{dbus.WithMatchSender(portalService), dbus.WithMatchInterface(requestInterface), dbus.WithMatchMember("Response")}
	if err := conn.AddMatchSignal(match...); err != nil {
		return nil, err
	}
	defer conn.RemoveMatchSignal(match...)
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	var handle dbus.ObjectPath
	call := conn.Object(portalService, portalPath).CallWithContext(ctx, printInterface+"."+method, 0, args...)
	if err := call.Store(&handle); err != nil {
		return nil, fmt.Errorf("desktop print dialog unavailable: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			conn.Object(portalService, handle).CallWithContext(cancelCtx, requestInterface+".Close", 0)
			cancel()
			return nil, ctx.Err()
		case signal, open := <-signals:
			if !open {
				return nil, fmt.Errorf("desktop printing service disconnected")
			}
			if signal == nil || signal.Path != handle || signal.Name != requestInterface+".Response" {
				continue
			}
			var response uint32
			var result map[string]dbus.Variant
			if err := dbus.Store(signal.Body, &response, &result); err != nil {
				return nil, err
			}
			switch response {
			case 0:
				return result, nil
			case 1:
				return nil, ErrCanceled
			default:
				return nil, fmt.Errorf("desktop printing service could not complete the request")
			}
		}
	}
}
