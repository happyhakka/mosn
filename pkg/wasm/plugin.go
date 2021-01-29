/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package wasm

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"

	v2 "mosn.io/mosn/pkg/config/v2"
	"mosn.io/mosn/pkg/log"
	"mosn.io/mosn/pkg/types"
)

var (
	ErrEngineNotFound = errors.New("fail to get wasm engine")
	ErrWasmBytesLoad  = errors.New("fail to load wasm bytes")
	ErrInstanceCreate = errors.New("fail to create wasm instance")
	ErrModuleCreate   = errors.New("fail to create wasm module")
)

type wasmPluginImpl struct {
	config v2.WasmPluginConfig

	lock sync.RWMutex

	instanceNum         int
	instanceWrappers    []types.WasmInstanceWrapper
	instanceWrappersIdx int32

	occupy int32

	vm        types.WasmVM
	wasmBytes []byte
	module    types.WasmModule
}

func NewWasmPlugin(wasmConfig v2.WasmPluginConfig) (types.WasmPlugin, error) {
	// check instance num
	instanceNum := wasmConfig.InstanceNum
	if instanceNum <= 0 {
		instanceNum = runtime.NumCPU()
	}
	wasmConfig.InstanceNum = instanceNum

	// get wasm engine
	vm := GetWasmEngine(wasmConfig.VmConfig.Engine)
	if vm == nil {
		log.DefaultLogger.Errorf("[wasm][plugin] NewWasmPlugin fail to get wasm engine: %v", wasmConfig.VmConfig.Engine)
		return nil, ErrEngineNotFound
	}

	// load wasm bytes
	var wasmBytes []byte
	if wasmConfig.VmConfig.Path != "" {
		wasmBytes = loadWasmBytesFromPath(wasmConfig.VmConfig.Path)
	} else {
		wasmBytes = loadWasmBytesFromUrl(wasmConfig.VmConfig.Url)
	}

	if wasmBytes == nil || len(wasmBytes) == 0 {
		log.DefaultLogger.Errorf("[wasm][plugin] NewWasmPlugin fail to load wasm bytes, config: %v", wasmConfig)
		return nil, ErrWasmBytesLoad
	}

	// create wasm module
	module := vm.NewModule(wasmBytes)
	if module == nil {
		log.DefaultLogger.Errorf("[wasm][plugin] NewWasmPlugin fail to create module, config: %v", wasmConfig)
		return nil, ErrModuleCreate
	}

	plugin := &wasmPluginImpl{
		config:    wasmConfig,
		vm:        vm,
		wasmBytes: wasmBytes,
		module:    module,
	}

	plugin.SetCpuLimit(wasmConfig.VmConfig.Cpu)
	plugin.SetMemLimit(wasmConfig.VmConfig.Mem)

	// ensure instance num
	actual := plugin.EnsureInstanceNum(wasmConfig.InstanceNum)
	if actual == 0 {
		log.DefaultLogger.Errorf("[wasm][plugin] NewWasmPlugin fail to ensure instance num, want: %v got 0", instanceNum)
		return nil, ErrInstanceCreate
	}

	return plugin, nil
}

// EnsureInstanceNum try to expand/shrink the num of instance to 'num'
// and return the actual instance num
func (w *wasmPluginImpl) EnsureInstanceNum(num int) int {
	if num <= 0 || num == w.instanceNum {
		return w.instanceNum
	}

	if num < w.instanceNum {
		w.lock.Lock()
		for i := num; i < len(w.instanceWrappers); i++ {
			w.instanceWrappers[i] = nil
		}
		w.instanceWrappers = w.instanceWrappers[:num]
		w.instanceNum = num
		w.lock.Unlock()
	} else {
		newInstance := make([]types.WasmInstanceWrapper, 0)
		numToCreate := num - w.instanceNum

		for i := 0; i < numToCreate; i++ {
			instance := w.module.NewInstance()
			if instance == nil {
				log.DefaultLogger.Errorf("[wasm][plugin] EnsureInstanceNum fail to create instance, i: %v", i)
				continue
			}
			newInstance = append(newInstance, &wasmInstanceWrapperImpl{WasmInstance: instance})
		}

		w.lock.Lock()
		w.instanceWrappers = append(w.instanceWrappers, newInstance...)
		w.instanceNum += len(newInstance)
		w.lock.Unlock()
	}

	return w.instanceNum
}

func (w *wasmPluginImpl) InstanceNum() int {
	return w.instanceNum
}

func (w *wasmPluginImpl) PluginName() string {
	return w.config.PluginName
}

func (w *wasmPluginImpl) Clear() {
	// do nothing
	return
}

// SetCpuLimit set cpu limit of the plugin, no-op
func (w *wasmPluginImpl) SetCpuLimit(cpu int) {
	return
}

// SetCpuLimit set cpu limit of the plugin, no-op
func (w *wasmPluginImpl) SetMemLimit(mem int) {
	return
}

// Exec execute the f for each instance
func (w *wasmPluginImpl) Exec(f func(instanceWrapper types.WasmInstanceWrapper) bool) {
	w.lock.RLock()
	defer w.lock.RUnlock()

	for _, iw := range w.instanceWrappers {
		if !f(iw) {
			break
		}
	}
}

func (w *wasmPluginImpl) GetPluginConfig() v2.WasmPluginConfig {
	return w.config
}

func (w *wasmPluginImpl) GetVmConfig() v2.WasmVmConfig {
	return *w.config.VmConfig
}

func (w *wasmPluginImpl) GetInstance() types.WasmInstanceWrapper {
	w.lock.RLock()
	defer w.lock.RUnlock()

	idx := int(w.instanceWrappersIdx) % len(w.instanceWrappers)

	iw := w.instanceWrappers[idx]

	w.instanceWrappersIdx++
	atomic.AddInt32(&w.occupy, 1)

	return iw
}

func (w *wasmPluginImpl) ReleaseInstance(instanceWrapper types.WasmInstanceWrapper) {
	atomic.AddInt32(&w.occupy, -1)
}

type DefaultWasmPluginHandler struct{}

func (d *DefaultWasmPluginHandler) OnConfigUpdate(config v2.WasmPluginConfig) {
	return
}

func (d *DefaultWasmPluginHandler) OnPluginStart(plugin types.WasmPlugin) {
	return
}

func (d *DefaultWasmPluginHandler) OnPluginDestroy(plugin types.WasmPlugin) {
	return
}