package mime

import (
	"bufio"
	"io"

	"github.com/gabriel-vasile/mimetype"
)

func Detect(rc io.Reader) (*mimetype.MIME, io.Reader, error) {
	// If it's already a bufio.Reader, don't wrap it again
	br, ok := rc.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(rc)
	}
	sniffLimit := 3072
	peekedBytes, err := br.Peek(sniffLimit)
	if err != nil && err != io.EOF {
		return nil, nil, err
	}

	return mimetype.Detect(peekedBytes), br, nil
}
