package tools

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// Supported text encodings. The empty string means UTF-8.
var supportedEncodings = map[string]encoding.Encoding{
	"gbk":          simplifiedchinese.GBK,
	"gb18030":      simplifiedchinese.GB18030,
	"big5":         traditionalchinese.Big5,
	"shift-jis":    japanese.ShiftJIS,
	"euc-jp":       japanese.EUCJP,
	"euc-kr":       korean.EUCKR,
	"latin1":       charmap.ISO8859_1,
	"windows-1252": charmap.Windows1252,
}

func lookupEncoding(name string) (encoding.Encoding, error) {
	if name == "" {
		return nil, nil
	}
	enc, ok := supportedEncodings[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("Unsupported encoding: %s", name)
	}
	return enc, nil
}

// decodeBytes decodes raw bytes using the given encoding ("" or "utf-8" is
// identity).
func decodeBytes(data []byte, enc string) (string, error) {
	e, err := lookupEncoding(enc)
	if err != nil {
		return "", err
	}
	if e == nil {
		return string(data), nil
	}
	decoded, err := e.NewDecoder().Bytes(data)
	if err != nil {
		return "", fmt.Errorf("Failed to decode %s: %v", enc, err)
	}
	return string(decoded), nil
}

// encodeBytes encodes text using the given encoding ("" or "utf-8" is
// identity).
func encodeBytes(text string, enc string) ([]byte, error) {
	e, err := lookupEncoding(enc)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return []byte(text), nil
	}
	encoded, err := e.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("Failed to encode %s: %v", enc, err)
	}
	return encoded, nil
}

// decodedReader wraps r so that bytes are decoded from the given encoding
// before being read ("" or "utf-8" is identity).
func decodedReader(r io.Reader, enc string) (io.Reader, error) {
	e, err := lookupEncoding(enc)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return r, nil
	}
	return transform.NewReader(r, e.NewDecoder()), nil
}
