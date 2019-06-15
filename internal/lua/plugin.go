package lua

import (
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"strings"

	lua "github.com/yuin/gopher-lua"
	"github.com/zyedidia/micro/internal/config"
)

var plugins []*Plugin

var ErrNoSuchFunction = errors.New("No such function exists")

func LoadAllPlugins() error {
	files, err := ioutil.ReadDir(path.Join(config.ConfigDir, "plugins"))
	if err != nil {
		return err
	}

	plugins = make([]*Plugin, len(files))
	for _, f := range files {
		if f.IsDir() {
			p, err := NewPluginFromDir(f.Name(), path.Join(config.ConfigDir, "plugins", f.Name()))
			if err != nil {
				return err
			}
			err = p.Load()
			if err != nil {
				return err
			}
			plugins = append(plugins, p)
		}
	}
	return err
}

type Plugin struct {
	Name  string
	files []string

	callbacks map[string]lua.LValue
}

func GetPlugin() {
	dbg, _ := L.GetStack(1)
	L.GetInfo("", dbg, lua.LNil)
	fmt.Println(dbg.What)
}

func (p *Plugin) RegisterCallback(name string, fn string) error {
	plug := L.GetGlobal(p.Name)
	luafn := L.GetField(plug, fn)
	if luafn == lua.LNil {
		return ErrNoSuchFunction
	}
	p.callbacks[name] = luafn
	return nil
}

func (p *Plugin) DoCallback(name string, args ...lua.LValue) (lua.LValue, error) {
	luafn, ok := p.callbacks[name]
	if !ok {
		return nil, ErrNoSuchFunction
	}

	err := L.CallByParam(lua.P{
		Fn:      luafn,
		NRet:    1,
		Protect: true,
	}, args...)
	if err != nil {
		return nil, err
	}
	ret := L.Get(-1)
	L.Pop(1)
	return ret, nil
}

func NewPluginFromDir(name string, dir string) (*Plugin, error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	p := new(Plugin)
	p.Name = name

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".lua") {
			p.files = append(p.files, path.Join(dir, f.Name()))
		}
	}

	return p, nil
}

func (p *Plugin) Load() error {
	for _, f := range p.files {
		dat, err := ioutil.ReadFile(f)
		if err != nil {
			return err
		}
		err = LoadFile(p.Name, f, dat)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Plugin) Call(fn string, args ...lua.LValue) (lua.LValue, error) {
	plug := L.GetGlobal(p.Name)
	luafn := L.GetField(plug, fn)
	if luafn == lua.LNil {
		return nil, ErrNoSuchFunction
	}
	err := L.CallByParam(lua.P{
		Fn:      luafn,
		NRet:    1,
		Protect: true,
	}, args...)
	if err != nil {
		return nil, err
	}
	ret := L.Get(-1)
	L.Pop(1)
	return ret, nil
}
