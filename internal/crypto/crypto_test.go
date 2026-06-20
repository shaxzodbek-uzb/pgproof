package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestPassphraseRoundTrip(t *testing.T) {
	s := Scheme{Passphrase: "correct horse battery staple"}
	plaintext := []byte("the quick brown fox jumps over 13 lazy dogs\n")

	var enc bytes.Buffer
	if _, err := s.Encrypt(&enc, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Contains(enc.Bytes(), plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}

	var dec bytes.Buffer
	if _, err := s.Decrypt(&dec, &enc); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(dec.Bytes(), plaintext) {
		t.Fatalf("round trip mismatch: %q", dec.Bytes())
	}
}

func TestWrongPassphraseFails(t *testing.T) {
	var enc bytes.Buffer
	if _, err := (Scheme{Passphrase: "right"}).Encrypt(&enc, strings.NewReader("data")); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	var dec bytes.Buffer
	if _, err := (Scheme{Passphrase: "wrong"}).Decrypt(&dec, &enc); err == nil {
		t.Fatal("expected decrypt failure with wrong passphrase")
	}
}

func TestRecipientRoundTrip(t *testing.T) {
	secret, public, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	encScheme := Scheme{Recipients: []string{public}}
	decScheme := Scheme{Identity: secret}

	plaintext := []byte("asymmetric secret payload")
	var enc bytes.Buffer
	if _, err := encScheme.Encrypt(&enc, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	var dec bytes.Buffer
	if _, err := decScheme.Decrypt(&dec, &enc); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(dec.Bytes(), plaintext) {
		t.Fatal("recipient round trip mismatch")
	}
}

func TestEnabled(t *testing.T) {
	if (Scheme{}).Enabled() {
		t.Error("empty scheme should be disabled")
	}
	if !(Scheme{Passphrase: "x"}).Enabled() {
		t.Error("passphrase scheme should be enabled")
	}
	if !(Scheme{Recipients: []string{"age1x"}}).Enabled() {
		t.Error("recipient scheme should be enabled")
	}
}
