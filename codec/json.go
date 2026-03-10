package codec

import (
	"bufio"
	"encoding/json"
	"io"
)

// JsonCodec
//
//	@Description: json codec实现
type JsonCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer
	dec  *json.Decoder
	enc  *json.Encoder
}

var _ Codec = (*JsonCodec)(nil) // 确保GobCodec实现了Codec接口

func NewJsonCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &JsonCodec{
		conn: conn,
		buf:  buf,
		dec:  json.NewDecoder(conn),
		enc:  json.NewEncoder(buf),
	}
}

func (c *JsonCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}
func (c *JsonCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *JsonCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	if err := c.enc.Encode(h); err != nil {
		return err
	}
	if err := c.enc.Encode(body); err != nil {
		return err
	}
	return nil
}

func (c *JsonCodec) Close() error {
	return c.conn.Close()
}
