package channel

import (
	"crypto/rand"
	"fmt"
	"strings"
	"unicode"
)

const (
	// joinCodeAlphabet 对应技术方案 §4.2：6 位，去掉易混的 0/O、1/I。
	// 长度是 32，能整除 256，单字节取模没有偏差。
	joinCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	joinCodeLength   = 6
	maxCodeAttempts  = 8
)

func newJoinCode() (string, error) {
	raw := make([]byte, joinCodeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, joinCodeLength)
	for i, b := range raw {
		out[i] = joinCodeAlphabet[int(b)%len(joinCodeAlphabet)]
	}
	return string(out), nil
}

// normalizeCode 去掉空白并转成大写，方便司机照着空格分开的口令输入。
func normalizeCode(raw string) string {
	out := make([]rune, 0, len(raw))
	for _, r := range raw {
		if unicode.IsSpace(r) {
			continue
		}
		out = append(out, unicode.ToUpper(r))
	}
	return string(out)
}

func validCode(code string) bool {
	if len(code) != joinCodeLength {
		return false
	}
	for _, r := range code {
		if !strings.ContainsRune(joinCodeAlphabet, r) {
			return false
		}
	}
	return true
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
