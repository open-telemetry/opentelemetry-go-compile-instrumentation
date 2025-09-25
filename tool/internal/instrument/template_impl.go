//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

//line <generated>:1
type HookContextImpl struct {
	Params      []interface{}
	ReturnVals  []interface{}
	SkipCall    bool
	Data        interface{}
	FuncName    string
	PackageName string
}

func (c *HookContextImpl) SetSkipCall(skip bool)    { c.SkipCall = skip }
func (c *HookContextImpl) IsSkipCall() bool         { return c.SkipCall }
func (c *HookContextImpl) SetData(data interface{}) { c.Data = data }
func (c *HookContextImpl) GetData() interface{}     { return c.Data }
func (c *HookContextImpl) GetKeyData(key string) interface{} {
	if c.Data == nil {
		return nil
	}
	return c.Data.(map[string]interface{})[key]
}

func (c *HookContextImpl) SetKeyData(key string, val interface{}) {
	if c.Data == nil {
		c.Data = make(map[string]interface{})
	}
	c.Data.(map[string]interface{})[key] = val
}

func (c *HookContextImpl) HasKeyData(key string) bool {
	if c.Data == nil {
		return false
	}
	_, ok := c.Data.(map[string]interface{})[key]
	return ok
}

func (c *HookContextImpl) GetParam(idx int) interface{} {
	switch idx {
	}
	return nil
}

func (c *HookContextImpl) SetParam(idx int, val interface{}) {
	if val == nil {
		c.Params[idx] = nil
		return
	}
	switch idx {
	}
}

func (c *HookContextImpl) GetReturnVal(idx int) interface{} {
	switch idx {
	}
	return nil
}

func (c *HookContextImpl) SetReturnVal(idx int, val interface{}) {
	if val == nil {
		c.ReturnVals[idx] = nil
		return
	}
	switch idx {
	}
}
func (c *HookContextImpl) GetParamCount() int     { return len(c.Params) }
func (c *HookContextImpl) GetReturnValCount() int { return len(c.ReturnVals) }
func (c *HookContextImpl) GetFuncName() string    { return c.FuncName }
func (c *HookContextImpl) GetPackageName() string { return c.PackageName }

// Variable Template
var (
	OtelGetStackImpl   func() []byte = nil
	OtelPrintStackImpl func([]byte)  = nil
)

// Trampoline Template
func OtelBeforeTrampoline() (HookContext, bool) {
	defer func() {
		if err := recover(); err != nil {
			println("failed to exec Before hook", "OtelBeforeNamePlaceholder")
			if e, ok := err.(error); ok {
				println(e.Error())
			}
			fetchStack, printStack := OtelGetStackImpl, OtelPrintStackImpl
			if fetchStack != nil && printStack != nil {
				printStack(fetchStack())
			}
		}
	}()
	hookContext := &HookContextImpl{}
	hookContext.Params = []interface{}{}
	hookContext.FuncName = ""
	hookContext.PackageName = ""
	return hookContext, hookContext.SkipCall
}

func OtelAfterTrampoline(hookContext HookContext) {
	defer func() {
		if err := recover(); err != nil {
			println("failed to exec After hook", "OtelAfterNamePlaceholder")
			if e, ok := err.(error); ok {
				println(e.Error())
			}
			fetchStack, printStack := OtelGetStackImpl, OtelPrintStackImpl
			if fetchStack != nil && printStack != nil {
				printStack(fetchStack())
			}
		}
	}()
	hookContext.(*HookContextImpl).ReturnVals = []interface{}{}
}
