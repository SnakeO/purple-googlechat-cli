// browser.go extracts fresh Google cookies directly from Chrome's cookie store.
// On macOS, Chrome encrypts cookies with a key stored in the Keychain.
// This module reads the SQLite database and decrypts cookie values.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/pbkdf2"
)

// ChromeProfile identifies a Chrome profile directory.
type ChromeProfile struct {
	Name string
	Path string
}

// ExtractChromeGoogleCookies reads and decrypts Google cookies from a Chrome profile.
// Returns a map of cookie name → value for .google.com and chat.google.com domains.
func ExtractChromeGoogleCookies(profilePath string) (map[string]string, error) {
	encKey, err := chromeEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("browser: cannot get Chrome encryption key: %w", err)
	}

	derivedKey := pbkdf2.Key(encKey, []byte("saltysalt"), 1003, 16, sha1.New)

	cookiesDB := filepath.Join(profilePath, "Cookies")
	db, err := sql.Open("sqlite3", cookiesDB+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("browser: cannot open Chrome cookies DB: %w", err)
	}
	defer db.Close()

	query := `SELECT name, encrypted_value, host_key, path FROM cookies
		WHERE (host_key = '.google.com' OR host_key = 'chat.google.com')
		AND name IN ('SID','HSID','SSID','OSID','COMPASS','SAPISID','APISID','SIDCC',
		'__Secure-1PSID','__Secure-3PSID','__Secure-1PAPISID','__Secure-3PAPISID',
		'__Secure-1PSIDCC','__Secure-3PSIDCC','__Secure-1PSIDTS','__Secure-3PSIDTS',
		'__Secure-OSID','NID','AEC','OTZ')`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("browser: cannot query cookies: %w", err)
	}
	defer rows.Close()

	cookies := make(map[string]string)
	for rows.Next() {
		var name string
		var encrypted []byte
		var host string
		var path string
		if err := rows.Scan(&name, &encrypted, &host, &path); err != nil {
			continue
		}

		value, err := decryptChromeValue(encrypted, derivedKey)
		if err != nil {
			continue
		}

		// For OSID and COMPASS, prefer chat.google.com domain
		key := name
		if name == "OSID" || name == "COMPASS" || name == "__Secure-OSID" {
			if host != "chat.google.com" {
				if _, exists := cookies[key]; exists {
					continue
				}
			}
		}

		// For COMPASS, prefer the one with path "/"
		if name == "COMPASS" && path != "/" {
			if _, exists := cookies[key]; exists {
				continue
			}
		}

		cookies[key] = value
	}

	return cookies, nil
}

// FindChromeProfiles returns all Chrome profiles that have chat.google.com cookies.
func FindChromeProfiles() ([]ChromeProfile, error) {
	chromeDir, err := chromeUserDataDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(chromeDir)
	if err != nil {
		return nil, fmt.Errorf("browser: cannot read Chrome directory: %w", err)
	}

	var profiles []ChromeProfile
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name(), "Profile") && entry.Name() != "Default" {
			continue
		}

		cookiesPath := filepath.Join(chromeDir, entry.Name(), "Cookies")
		if _, err := os.Stat(cookiesPath); err != nil {
			continue
		}

		db, err := sql.Open("sqlite3", cookiesPath+"?mode=ro")
		if err != nil {
			continue
		}

		var count int
		db.QueryRow("SELECT count(*) FROM cookies WHERE host_key = 'chat.google.com'").Scan(&count)
		db.Close()

		if count > 0 {
			profiles = append(profiles, ChromeProfile{
				Name: entry.Name(),
				Path: filepath.Join(chromeDir, entry.Name()),
			})
		}
	}

	return profiles, nil
}

// chromeUserDataDir returns the Chrome user data directory for the current OS.
func chromeUserDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Google", "Chrome"), nil
}

// chromeEncryptionKey retrieves Chrome's cookie encryption key from macOS Keychain.
func chromeEncryptionKey() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password", "-w", "-s", "Chrome Safe Storage", "-a", "Chrome").Output()
	if err != nil {
		return nil, fmt.Errorf("cannot read Chrome Safe Storage from Keychain: %w", err)
	}
	return []byte(strings.TrimSpace(string(out))), nil
}

// decryptChromeValue decrypts a Chrome encrypted cookie value.
// Chrome on macOS uses AES-128-CBC with PBKDF2-derived key and "saltysalt".
func decryptChromeValue(encrypted []byte, key []byte) (string, error) {
	if len(encrypted) == 0 {
		return "", nil
	}

	// v10 prefix = Chrome encryption version
	if len(encrypted) < 3 || string(encrypted[:3]) != "v10" {
		return string(encrypted), nil
	}
	encrypted = encrypted[3:]

	if len(encrypted) < aes.BlockSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	// Chrome uses a fixed IV of 16 spaces
	iv := []byte("                ")
	mode := cipher.NewCBCDecrypter(block, iv)

	decrypted := make([]byte, len(encrypted))
	mode.CryptBlocks(decrypted, encrypted)

	// Remove PKCS7 padding
	if len(decrypted) > 0 {
		padLen := int(decrypted[len(decrypted)-1])
		if padLen > 0 && padLen <= aes.BlockSize && padLen <= len(decrypted) {
			decrypted = decrypted[:len(decrypted)-padLen]
		}
	}

	// Chrome prepends a 32-byte header to decrypted values.
	// Find the start of printable ASCII content.
	result := stripNonPrintablePrefix(decrypted)
	return result, nil
}

// stripNonPrintablePrefix removes the binary prefix Chrome adds to decrypted cookies.
// Chrome's v10 encrypted cookies have a 32-byte header before the actual value.
func stripNonPrintablePrefix(data []byte) string {
	for i, b := range data {
		if b >= 0x20 && b < 0x7F {
			// Check if the rest looks like a valid cookie value
			valid := true
			for j := i; j < len(data) && j < i+10; j++ {
				if data[j] < 0x20 || data[j] >= 0x7F {
					valid = false
					break
				}
			}
			if valid {
				return string(data[i:])
			}
		}
	}
	return string(data)
}
