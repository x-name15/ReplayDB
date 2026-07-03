package wire

import (
	"crypto/subtle"
	"io"
)

const maxTokenLen = 4096

func WriteAuthToken(w io.Writer, token string) error {
	return writeFrame(w, []byte(token))
}

func ReadAuthToken(r io.Reader) (string, error) {
	frame, err := readFrame(r)
	if err != nil {
		return "", err
	}
	if len(frame) > maxTokenLen {
		return "", io.ErrShortBuffer
	}
	return string(frame), nil
}

func TokensEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
