package goRpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

// methodType
// @Description: 方法类型，包含方法反射信息、参数类型、返回值类型、调用次数
type methodType struct {
	method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint64
}

// NumCalls
//
//	@Description: 原子获取方法调用次数
//	@receiver m 方法类型实例
//	@return uint64 方法调用次数
func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

// newArgv
//
//	@Description: 创建方法的参数值
//	@receiver m 方法类型实例
//	@return reflect.Value 方法参数值
func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	if m.ArgType.Kind() == reflect.Ptr {
		// 如果参数是指针类型，创建指针指向的零值
		argv = reflect.New(m.ArgType.Elem())
	} else {
		// 如果参数不是指针类型，创建参数的零值
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

// newReplyv
//
//	@Description: 创建方法的返回值值
//	@receiver m 方法类型实例
//	@return reflect.Value 方法返回值值
func (m *methodType) newReplyv() reflect.Value {
	replyv := reflect.New(m.ReplyType.Elem())
	// 根据返回值类型创建返回值值
	switch m.ReplyType.Elem().Kind() {
	// 如果返回值是映射类型，创建映射类型的零值
	case reflect.Map:
		replyv = reflect.MakeMap(m.ReplyType.Elem())
	// 如果返回值是切片类型，创建切片类型的零值
	case reflect.Slice:
		replyv = reflect.MakeSlice(m.ReplyType.Elem(), 0, 0)
	}
	return replyv
}

// service
// @Description: 服务类型，包含服务名称、服务反射类型、服务实例、方法映射
type service struct {
	name   string
	typ    reflect.Type
	rcvr   reflect.Value
	method map[string]*methodType
}

func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %v is not exported", s.name)
	}
	s.registerMethods()
	return s
}

// registerMethods
//
//	@Description: 注册服务的方法
//	@receiver s
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// call
//
//	@Description: 调用服务的方法
//	@receiver s 服务类型实例
//	@param m 方法类型实例
//	@param argv 方法参数值
//	@param replyv 方法返回值值
//	@return error 调用方法时可能返回的错误
func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
