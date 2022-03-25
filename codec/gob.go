package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn io.ReadWriteCloser //conn 是由构建函数传入，通常是通过 TCP 或者 Unix 建立 socket 时得到的链接实例
	buf  *bufio.Writer      //buf 是为了防止阻塞而创建的带缓冲的 Writer，一般这么做能提升性能
	dec  *gob.Decoder       //dec 和 enc 对应 gob 的 Decoder 和 Encoder
	enc  *gob.Encoder
}

var _ Codec = (*GobCodec)(nil)

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	//encoder 因为是要往 conn 中写入内容, 可以使用 buffer 来优化写入效率, 所以我们先写入到 buffer 中,
	//然后我们再调用 buffer.Flush() 来将 buffer 中的全部内容写入到 conn 中, 从而优化效率.
	//对于读则不需要这方面的考虑, 所以直接在 conn 中读内容即可.
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

// ReadHeader 解码之后存入h中
func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(header *Header, body interface{}) (err error) {
	//调用 buffer.Flush() 来将 buffer 中的全部内容写入到 conn 中, 从而优化效率.
	defer func() {
		c.buf.Flush()
		if err != nil {
			c.Close()
		}
	}()

	if err := c.enc.Encode(header); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}

	if err := c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}

	return nil

}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}
