package wasmer

// #include <wasmer_wasm.h>
import "C"
import "runtime"

type Instance struct {
	_inner  *C.wasm_instance_t
	Exports *Exports
}

// NewInstance instantiates a new Instance.
//
// It takes two arguments, the Module and an ImportObject.
//
// ⚠️ Instantiating a module may return TrapError if the module's start function traps.
//
//   wasmBytes := []byte(`...`)
//   engine := wasmer.NewEngine()
//	 store := wasmer.NewStore(engine)
//	 module, err := wasmer.NewModule(store, wasmBytes)
//   importObject := wasmer.NewImportObject()
//   instance, err := wasmer.NewInstance(module, importObject)
//
func NewInstance(module *Module, imports *ImportObject) (*Instance, error) {
	var traps *C.wasm_trap_t
	externs, err := imports.intoInner(module)

	if err != nil {
		return nil, err
	}

	instance := C.wasm_instance_new(
		module.store.inner(),
		module.inner(),
		externs,
		&traps,
	)

	runtime.KeepAlive(module)
	runtime.KeepAlive(module.store)
	runtime.KeepAlive(imports)

	if instance == nil {
		return nil, newErrorFromWasmer()
	}

	if traps != nil {
		// TODO(jubianchi): Implement this properly
		return nil, newErrorWith("trapped! to do")
	}

	output := &Instance{
		_inner:  instance,
		Exports: newExports(instance, module),
	}

	runtime.SetFinalizer(output, func(self *Instance) {
		C.wasm_instance_delete(self.inner())
	})

	return output, nil
}

func (self *Instance) inner() *C.wasm_instance_t {
	return self._inner
}