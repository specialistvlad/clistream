package main

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/identity"
)

func TestReconnectSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, first := range []time.Duration{0, time.Second} {
			var waits []time.Duration
			last := time.Now()
			_, err := retry(t.Context(), &console{}, first, func(context.Context) (*quic.Conn, error) {
				now := time.Now()
				waits = append(waits, now.Sub(last))
				last = now
				if len(waits) == 8 {
					return nil, nil
				}
				return nil, errors.New("offline")
			})
			want := []time.Duration{0, 1, 2, 4, 8, 16, 30, 30}
			if first != 0 {
				want = []time.Duration{1, 2, 4, 8, 16, 30, 30, 30}
			}
			for i := range want {
				want[i] *= time.Second
			}
			if err != nil || !slices.Equal(waits, want) {
				t.Fatalf("waits %v, error %v", waits, err)
			}
		}
	})
}
func TestReconnectCancellationAndRejection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(time.Second, cancel)
		_, err := retry(ctx, &console{}, 30*time.Second, func(context.Context) (*quic.Conn, error) { t.Fatal("dial after cancellation"); return nil, nil })
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		for _, cause := range []error{identity.ErrServerKeyMismatch, identity.ErrDeviceKey, errLegacyRole, denied{"authorization required"}} {
			calls := 0
			_, err := retry(t.Context(), &console{}, 0, func(context.Context) (*quic.Conn, error) {
				calls++
				if calls > 1 {
					t.Fatal("retried permanent error")
				}
				return nil, cause
			})
			if !errors.Is(err, cause) {
				t.Fatal(err)
			}
		}
	})
}
