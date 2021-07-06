package react

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strings"

	"github.com/dop251/goja"
)

//go:embed assets/babel.js
var babelSource []byte

type JSX struct {
	pool *Pool
	opt  *Option
}

func NewJSX() (*JSX, error) {
	InitBabel()
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

func DefaultJSXOption() *Option {
	return &Option{
		Source:           babelSource,
		PoolSize:         2,
		GlobalObjectName: "self",
	}
}

type babelTransformer struct {
	Runtime   *goja.Runtime
	Transform func(string, map[string]interface{}) (goja.Value, error)
}

var babelProg *goja.Program
var babel *babelTransformer

func (jsx *JSX) TransformFile(path string) (string, error) {
	src, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	res, err := babelTransformString(string(src), map[string]interface{}{
		"plugins": []string{
			"transform-react-jsx",
			"transform-block-scoping",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	return res, nil
}

func logFunc(goja.FunctionCall) goja.Value {
	return nil
}

func newBabelVM() *goja.Runtime {
	vm := goja.New()
	// define console.{log|error|warn} so loading babel doesn't error
	vm.Set("console", map[string]func(goja.FunctionCall) goja.Value{
		"log":   logFunc,
		"error": logFunc,
		"warn":  logFunc,
	})
	return vm
}

func InitBabel() (err error) {
	if e := compileBabel(); e != nil {
		err = e
		return
	}
	vm := newBabelVM()

	transformFn, e := loadBabel(vm)
	if e != nil {
		err = e
		return
	}
	babel = &babelTransformer{
		Runtime:   vm,
		Transform: transformFn,
	}

	return nil
}

func Transform(src io.Reader, opts map[string]interface{}) (io.Reader, error) {
	data, err := ioutil.ReadAll(src)
	if err != nil {
		return nil, err
	}
	res, err := babelTransformString(string(data), opts)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(res), nil
}

func babelTransformString(src string, opts map[string]interface{}) (string, error) {
	if opts == nil {
		opts = map[string]interface{}{}
	}
	v, err := babel.Transform(src, opts)
	if err != nil {
		return "", err
	}
	vm := babel.Runtime
	return v.ToObject(vm).Get("code").String(), nil
}

func compileBabel() error {
	var err error
	babelProg, err = goja.Compile("babel.js", string(babelSource), false)
	if err != nil {
		return err
	}
	return nil
}

func loadBabel(vm *goja.Runtime) (func(string, map[string]interface{}) (goja.Value, error), error) {
	_, err := vm.RunProgram(babelProg)
	if err != nil {
		return nil, fmt.Errorf("unable to load babel.js: %s", err)
	}
	var transform goja.Callable
	babel := vm.Get("Babel")
	if err := vm.ExportTo(babel.ToObject(vm).Get("transform"), &transform); err != nil {
		return nil, fmt.Errorf("unable to export transform fn: %s", err)
	}
	return func(src string, opts map[string]interface{}) (goja.Value, error) {
		return transform(babel, vm.ToValue(src), vm.ToValue(opts))
	}, nil
}
