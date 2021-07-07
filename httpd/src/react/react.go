package react

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"runtime"

	"github.com/dop251/goja"
)

type React struct {
	pool *Pool
	opt  *Option
}

// Create a new react object.
func NewReact() (*React, error) {
	return NewReactWithOption(DefaultReactOption())
}

// Create a new react object using option.
// opt: Option for react object.
func NewReactWithOption(opt *Option) (*React, error) {
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

	return &React{pool: pool, opt: opt}, nil
}

//go:embed assets/react.js
var reactSource []byte

// Returns a default option for react.
func DefaultReactOption() *Option {
	return &Option{
		Source:           reactSource,
		PoolSize:         runtime.NumCPU(),
		GlobalObjectName: "self",
	}
}

// Render react component.
// name: component name
// params: component properties
func (rc *React) RenderComponent(name string, params interface{}) (string, error) {
	vm := rc.pool.Get()
	defer rc.pool.Put(vm)

	objName := rc.opt.GlobalObjectName

	var js string
	if params == nil {
		js = fmt.Sprintf(`
			%v.React.renderToString(
				%v.React.createFactory(%v.%v)()
			)`, objName, objName, objName, name)
	} else {
		j, err := json.Marshal(params)
		if err != nil {
			return "", err
		}
		js = fmt.Sprintf(`
			%v.React.renderToString(
				%v.React.createFactory(%v.%v)(%v)
			)`, objName, objName, objName, name, string(j))
	}

	v, err := vm.RunString(js)
	if err != nil {
		return "", fmt.Errorf("RenderComponent: %s", err)
	}
	return v.String(), nil
}

// Load javascript code.
// src: javascript source
func (rc *React) Load(src []byte) error {
	for i := 0; i < rc.pool.size; i++ {
		vm := rc.pool.Get()
		defer rc.pool.Put(vm)

		vm.RunString("var self = this")

		prog, err := goja.Compile("react.js", string(src), false)
		if err != nil {
			return err
		}
		if _, err := vm.RunProgram(prog); err != nil {
			return fmt.Errorf("unable to load react.js: %s", err)
		}
	}
	return nil
}

// Load javascript file.
// path: path for javascript source file
func (rc *React) LoadFile(path string) error {
	src, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	return rc.Load(src)
}
