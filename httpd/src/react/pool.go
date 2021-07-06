package react

import (
	"errors"

	"github.com/dop251/goja"
)

type Pool struct {
	size int
	ch   chan *goja.Runtime
}

type Option struct {
	Source []byte
	// size for javascript vm pool.
	PoolSize int
	// name for variable includes component objects. ex. "self"
	GlobalObjectName string
}

func (opt *Option) Validate() error {
	if opt.Source == nil {
		return errors.New("react: nil []byte opt.Source")
	}
	if opt.PoolSize <= 0 {
		return errors.New("react: opt.PoolSize must be greater than or equal to 1")
	}
	if opt.GlobalObjectName == "" {
		return errors.New("react: empty string opt.GlobalObjectName")
	}
	return nil
}

func NewPool(opt *Option) (*Pool, error) {
	pool := &Pool{size: opt.PoolSize}
	pool.ch = make(chan *goja.Runtime, opt.PoolSize)
	for i := 0; i < pool.size; i++ {
		vm := newVM(opt.Source, opt.GlobalObjectName)
		pool.ch <- vm
	}
	return pool, nil
}

func newVM(src []byte, objName string) *goja.Runtime {
	vm := goja.New()
	// define console.{log|error|warn} so loading babel doesn't error
	vm.Set("console", map[string]func(goja.FunctionCall) goja.Value{
		"log":   logFunc,
		"error": logFunc,
		"warn":  logFunc,
	})
	_, err := vm.RunScript("react.js", string(src))
	if err != nil {
		panic(err)
	}
	return vm
}

func (pl *Pool) Get() *goja.Runtime {
	return <-pl.ch
}

func (pl *Pool) Put(vm *goja.Runtime) {
	pl.ch <- vm
}
