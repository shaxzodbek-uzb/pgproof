// Package crypto provides streaming at-rest encryption of backup artifacts
// using the age format (https://age-encryption.org). Two modes are supported:
//
//   - passphrase: symmetric scrypt-based encryption (simplest to operate).
//   - recipients: asymmetric X25519 public keys (the secret key never has to
//     live on the backup host; only on the machine that restores).
//
// The package only ever streams, so multi-gigabyte dumps are encrypted without
// being held in memory.
package crypto

import (
	"fmt"
	"io"

	"filippo.io/age"
)

// Extension is appended to encrypted artifact names.
const Extension = ".age"

// Scheme identifies how artifacts are encrypted.
type Scheme struct {
	Passphrase string
	Recipients []string // age X25519 public recipients ("age1...")
	Identity   string   // age secret key ("AGE-SECRET-KEY-...") for decryption
}

// Enabled reports whether encryption is configured.
func (s Scheme) Enabled() bool {
	return s.Passphrase != "" || len(s.Recipients) > 0
}

// Encrypt streams src into dst, encrypting with the configured scheme.
// It returns the number of plaintext bytes written.
func (s Scheme) Encrypt(dst io.Writer, src io.Reader) (int64, error) {
	recipients, err := s.recipients()
	if err != nil {
		return 0, err
	}
	w, err := age.Encrypt(dst, recipients...)
	if err != nil {
		return 0, fmt.Errorf("init age encryption: %w", err)
	}
	n, err := io.Copy(w, src)
	if err != nil {
		_ = w.Close()
		return n, fmt.Errorf("encrypt stream: %w", err)
	}
	if err := w.Close(); err != nil {
		return n, fmt.Errorf("finalize age stream: %w", err)
	}
	return n, nil
}

// Decrypt streams src into dst, decrypting with the configured scheme.
func (s Scheme) Decrypt(dst io.Writer, src io.Reader) (int64, error) {
	identities, err := s.identities()
	if err != nil {
		return 0, err
	}
	r, err := age.Decrypt(src, identities...)
	if err != nil {
		return 0, fmt.Errorf("init age decryption (wrong passphrase/key?): %w", err)
	}
	n, err := io.Copy(dst, r)
	if err != nil {
		return n, fmt.Errorf("decrypt stream: %w", err)
	}
	return n, nil
}

func (s Scheme) recipients() ([]age.Recipient, error) {
	if s.Passphrase != "" {
		r, err := age.NewScryptRecipient(s.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("passphrase recipient: %w", err)
		}
		return []age.Recipient{r}, nil
	}
	if len(s.Recipients) == 0 {
		return nil, fmt.Errorf("no encryption recipients configured")
	}
	out := make([]age.Recipient, 0, len(s.Recipients))
	for _, raw := range s.Recipients {
		r, err := age.ParseX25519Recipient(raw)
		if err != nil {
			return nil, fmt.Errorf("parse recipient %q: %w", raw, err)
		}
		out = append(out, r)
	}
	return out, nil
}

func (s Scheme) identities() ([]age.Identity, error) {
	if s.Passphrase != "" {
		id, err := age.NewScryptIdentity(s.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("passphrase identity: %w", err)
		}
		return []age.Identity{id}, nil
	}
	if s.Identity == "" {
		return nil, fmt.Errorf("no decryption identity (age secret key) configured")
	}
	id, err := age.ParseX25519Identity(s.Identity)
	if err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	return []age.Identity{id}, nil
}

// GenerateKeypair returns a fresh age X25519 (secret, public) pair.
func GenerateKeypair() (secret, public string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.String(), id.Recipient().String(), nil
}
