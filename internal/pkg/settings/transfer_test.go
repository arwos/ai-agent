package settings

import "testing"

func TestSecretRoundTrip(t *testing.T) {
	encoded := EncodeSecret("пароль / api-key")
	decoded, err := DecodeSecret(encoded)
	if err != nil || decoded != "пароль / api-key" {
		t.Fatalf("round trip failed: %q, %v", decoded, err)
	}
}

func TestBundleValidation(t *testing.T) {
	b, err := Unmarshal([]byte(`{"format":"arwos-settings","version":1}`))
	if err != nil || b.Format != Format {
		t.Fatalf("unexpected bundle: %#v, %v", b, err)
	}
	if _, err = Unmarshal([]byte(`{"format":"other","version":1}`)); err == nil {
		t.Fatal("expected format validation error")
	}
}
