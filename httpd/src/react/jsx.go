package react

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
)

type JSX struct {
	pool *Pool
	opt  *Option
}

func NewJSX() (*JSX, error) {
	return NewJSXWithOption(DefaultJSXOption())
}

func NewJSXWithOption(opt *Option) (*JSX, error) {
	if opt == nil {
		return nil, errors.New("react: nil opt *Option")
	}
	err := opt.Validate()
	if err != nil {
		return nil, err
	}

	pool, err := NewPool(opt)
	if err != nil {
		return nil, err
	}

	return &JSX{pool: pool, opt: opt}, nil
}

//go:embed assets/JSXTransformer.js
var jsxSource []byte

func DefaultJSXOption() *Option {
	return &Option{
		Source:           jsxSource,
		PoolSize:         10,
		GlobalObjectName: "self",
	}
}

func (jx *JSX) Transform(source []byte, opt map[string]interface{}) ([]byte, error) {
	optJSON, err := json.Marshal(opt)
	if err != nil {
		return nil, err
	}
	vm := jx.pool.Get()
	defer jx.pool.Put(vm)

	vm.PevalString(fmt.Sprintf(`(function(){return %v.JSXTransformer.transform(%#v, %v)['code'];})();`, jx.opt.GlobalObjectName, source, string(optJSON)))
	v := vm.SafeToString(-1)
	vm.Pop()
	return []byte(v), nil
}

func (jx *JSX) TransformFile(path string, opt map[string]interface{}) ([]byte, error) {
	src, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	optJSON, err := json.Marshal(opt)
	if err != nil {
		return nil, err
	}
	vm := jx.pool.Get()
	defer jx.pool.Put(vm)

	vm.PevalString(fmt.Sprintf(`(function(){return %v.JSXTransformer.transform(%#v, %v)['code'];})();`, jx.opt.GlobalObjectName, string(src), string(optJSON)))
	v := vm.SafeToString(-1)
	vm.Pop()
	return []byte(v), nil
}
