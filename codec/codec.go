package codec

import "io"

// Header
//
//	@Description: 消息头，包含服务名、方法名、请求序列号和错误信息
type Header struct {
	ServiceMethod string // format: "Service.Method"
	Seq           uint64 // sequence number chosen by client
	Error         string // error message
}

// Codec
//
//	@Description: 编解码接口，用于实现不同的codec实例
type Codec interface {
	io.Closer                         // close the connection
	ReadHeader(*Header) error         // read header from connection
	ReadBody(interface{}) error       // read body from connection
	Write(*Header, interface{}) error // write header and body to connection
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

const (
	JsonType Type = "application/json" //todo: 实现json codec
	GobType  Type = "application/gob"
)

var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = map[Type]NewCodecFunc{}
	NewCodecFuncMap[GobType] = NewGobCodec
}
