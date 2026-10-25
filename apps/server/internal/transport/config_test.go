package transport

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/viper"
)

func TestManagedCertificateDefaultLivesUnderState(t *testing.T) {
	t.Parallel()

	want := filepath.Join(".state", "cluster", "dev-certs")
	if got := DefaultConfig().CertDir; got != want {
		t.Fatalf("development certificate directory = %q, want %q", got, want)
	}
}

func TestConfigFromViperSplitsEnvironmentOriginCSV(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv(
		"TRANSPORT_ALLOWED_ORIGINS",
		" http://127.0.0.1:5174, http://localhost:5174 ",
	)

	RegisterViperDefaults()
	got := ConfigFromViper().AllowedOrigins
	want := []string{
		"http://127.0.0.1:5174",
		"http://localhost:5174",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed origins = %#v, want %#v", got, want)
	}
}

func TestConfiguredStringListPreservesNativeSlice(t *testing.T) {
	t.Parallel()

	got := configuredStringList([]string{
		"https://one.example",
		" https://two.example ",
		"",
	})
	want := []string{
		"https://one.example",
		"https://two.example",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configured string list = %#v, want %#v", got, want)
	}
}

func TestWebTransportPublicEnabledVocabulary(t *testing.T) {
	for value, want := range map[string]bool{
		"on": true, "ON": true, " on ": true, "1": true, "true": true,
		"off": false, "0": false, "false": false, "": false, "yes": false,
	} {
		t.Setenv(EnvWebTransportPublic, value)
		if got := WebTransportPublicEnabled(); got != want {
			t.Fatalf("%s=%q: got %v, want %v", EnvWebTransportPublic, value, got, want)
		}
	}
}

func TestConfigFromViperReadsOutboundQueueBounds(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TRANSPORT_OUTBOUND_QUEUE", "2048")
	t.Setenv("TRANSPORT_OUTBOUND_QUEUE_BYTES", "25165824")

	RegisterViperDefaults()
	got := ConfigFromViper()
	if got.OutboundQueue != 2048 || got.OutboundQueueBytes != 24<<20 {
		t.Fatalf("outbound bounds = %d frames/%d bytes, want 2048/%d", got.OutboundQueue, got.OutboundQueueBytes, 24<<20)
	}
}
