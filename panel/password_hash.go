package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

var passwordWork = make(chan struct{}, 2)

func hashPassword(password string) (string, error) {
	if len(password) == 0 || len(password) > 4096 {
		return "", fmt.Errorf("invalid password length")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	passwordWork <- struct{}{}
	defer func() { <-passwordWork }()
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) (valid, legacy bool) {
	if len(password) > 4096 {
		return false, false
	}
	if len(encoded) == 64 && !strings.HasPrefix(encoded, "$") {
		want, err := hex.DecodeString(encoded)
		if err != nil {
			return false, false
		}
		got := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare(want, got[:]) == 1, true
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, false
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return false, false
	}
	values := make([]int, 3)
	for i, prefix := range []string{"m=", "t=", "p="} {
		if !strings.HasPrefix(params[i], prefix) {
			return false, false
		}
		v, e := strconv.Atoi(strings.TrimPrefix(params[i], prefix))
		if e != nil {
			return false, false
		}
		values[i] = v
	}
	if values[0] < 8192 || values[0] > 131072 || values[1] < 1 || values[1] > 6 || values[2] < 1 || values[2] > 4 {
		return false, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false, false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) != 32 {
		return false, false
	}
	passwordWork <- struct{}{}
	defer func() { <-passwordWork }()
	got := argon2.IDKey([]byte(password), salt, uint32(values[1]), uint32(values[0]), uint8(values[2]), 32)
	return subtle.ConstantTimeCompare(want, got) == 1, false
}
