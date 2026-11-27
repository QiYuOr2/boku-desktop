// Package settings 把应用配置存成用户数据目录下的单个文件 settings.json。
//
// 存储本身不知道里面有什么：每个有设置的功能注册一个 Section，声明自己的名字
// 以及如何读写自己的键。功能只能拿到自己那份，无法经此触及其他功能的设置。
//
// 经 atomicfile 写入，写到一半崩溃会保留旧文件。读不出的文件按错误处理，
// 否则界面会显示用户从未选过的默认值再写回磁盘。
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"boku/internal/kit"
	"boku/internal/platform/atomicfile"
)

// FileVersion 是当前构建读写的文件版本；版本不符的文件会被拒绝，而非被部分解读。
const FileVersion = 1

// Section 是一个功能的配置，以该功能自己的方式表示。
//
// 存储从不查看其内部：二者是文件格式与功能类型唯一的交界，新增设置无需改动本包。
// 文件是扁平对象，各个 Decode 拿到的是全部功能的键，只取自己的键是各 Section 的纪律；
// Encode 交回的键会合并进文件，交回别人的键就等于写了不归自己管的设置。
type Section interface {
	// Name 仅用于错误信息，不是文件里的键。
	Name() string
	// Decode 缺失的键保持原样，使早期写入的文件给出默认值而非零值。
	Decode(values map[string]json.RawMessage) error
	// Encode 返回本功能拥有的键。
	Encode() (map[string]any, error)
}

// Store 对应 settings.json，可在多个 goroutine 中安全使用。
type Store struct {
	Path string

	// mu 保护文件、sections 及每次读-改-写：并发改动不丢失的前提是读与写之间持同一把锁。
	mu       sync.Mutex
	sections []Section
	byName   map[string]Section
	// unclaimed 保存文件里无人认领的键（手写的或某 Section 后来放弃的），按原样写回，
	// 避免改一个设置就缩小用户正在编辑的文件。
	unclaimed map[string]json.RawMessage
}

// document 是 settings.json 的结构：版本号，以及各功能共享的扁平对象。
//
// 所有功能的键都平铺在同一个 "tools" 对象里。这是现有文件的形状，保持它即可
// 无需认识字段、也无需任何迁移。
type document struct {
	Version int                        `json:"version"`
	Tools   map[string]json.RawMessage `json:"tools"`
}

// New 在给定路径上创建 Store，并注册要读写的 sections。
// 文件不存在不算错误：各 Section 保留默认值，首次改动时再写入。
func New(path string, sections ...Section) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("settings: the store has no path")
	}
	store := &Store{Path: path, byName: make(map[string]Section, len(sections))}
	for _, section := range sections {
		if section == nil {
			return nil, fmt.Errorf("settings: a section is nil")
		}
		name := section.Name()
		if name == "" {
			return nil, fmt.Errorf("settings: a section has no name")
		}
		if _, twice := store.byName[name]; twice {
			return nil, fmt.Errorf("settings: section %s is registered twice", name)
		}
		store.byName[name] = section
		store.sections = append(store.sections, section)
	}
	return store, nil
}

// Load 把文件读入所有已注册的 Section。
//
// 文件里没有的键保持原样（即功能默认值）：这是从未保存过设置的用户应有的结果，
// 也是新增设置无需迁移的原因。
func (s *Store) Load(ctx context.Context) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// read 把文件读入每个 Section。
//
// Decode 中途失败时，失败前的 Section 已按文件取值、其后的还没有，两者会不一致。
// 文件本身没错、错在 Section，这里只能把错误告诉调用方；Section 若在意，
// 应自己保证 Decode 的原子性。
func (s *Store) read() error {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("settings: read %s: %w", s.Path, err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("settings: %s is not a readable settings file: %w", s.Path, err)
	}
	if doc.Version != FileVersion {
		return fmt.Errorf("settings: %s is version %d, this build reads version %d",
			s.Path, doc.Version, FileVersion)
	}
	for _, section := range s.sections {
		if err := section.Decode(doc.Tools); err != nil {
			return fmt.Errorf("settings: %s: %s: %w", s.Path, section.Name(), err)
		}
	}
	s.rememberUnclaimed(doc.Tools)
	return nil
}

// rememberUnclaimed 保留文件里无人认领的键，以便保存时放回。必须在所有 Decode
// 之后运行：一个 Section 拥有哪些键，要等所有 Section 都读过文件才知道。
func (s *Store) rememberUnclaimed(tools map[string]json.RawMessage) {
	if len(tools) == 0 {
		return
	}
	claimed := make(map[string]bool, len(tools))
	for _, section := range s.sections {
		values, err := section.Encode()
		if err != nil {
			// 说不清自己拥有哪些键的 Section 会把问题留到即将进行的写入处报错。
			continue
		}
		for key := range values {
			claimed[key] = true
		}
	}
	unclaimed := make(map[string]json.RawMessage)
	for key, raw := range tools {
		if !claimed[key] {
			unclaimed[key] = raw
		}
	}
	if len(unclaimed) == 0 {
		s.unclaimed = nil
		return
	}
	s.unclaimed = unclaimed
}

// Update 读取文件，交给一个 Section 修改，再把整份文档写回。
//
// 这是改动设置的唯一入口。若用 Load 读、改、再 Save 写，两个调用方会互相覆盖：
// 双方都读到旧文件、各写回「读到的 + 自己的改动」，后写的撤销先写的。放进同一临界区即可避免。
//
// change 只作用于指定 Section；其余 Section 从文件重读并写回，因此一个功能的改动
// 不会带回另一个功能的旧值。
func (s *Store) Update(ctx context.Context, name string, change func() error) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	if _, known := s.byName[name]; !known {
		return fmt.Errorf("settings: no section is named %s", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.read(); err != nil {
		return err
	}
	if err := change(); err != nil {
		return err
	}
	return s.write()
}

// Save 按当前状态写出所有 Section。
func (s *Store) Save(ctx context.Context) error {
	if err := kit.ContextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write()
}

func (s *Store) write() error {
	tools := make(map[string]json.RawMessage)
	for _, section := range s.sections {
		values, err := section.Encode()
		if err != nil {
			return fmt.Errorf("settings: encode %s: %w", section.Name(), err)
		}
		for key, value := range values {
			raw, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("settings: encode %s: %s: %w", section.Name(), key, err)
			}
			// 两个功能写同一个键属于装配错误，不能静默取舍：谁赢只会取决于注册顺序。
			if _, taken := tools[key]; taken {
				return fmt.Errorf("settings: %s and another section both write %q", section.Name(), key)
			}
			tools[key] = raw
		}
	}
	// 无人认领的键按原样放回；已被 Section 认领的键归该 Section 写，不从这里放回。
	for key, raw := range s.unclaimed {
		if _, claimed := tools[key]; claimed {
			continue
		}
		tools[key] = raw
	}
	doc := document{Version: FileVersion, Tools: tools}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: encode %s: %w", s.Path, err)
	}
	if err := atomicfile.Write(s.Path, append(data, '\n')); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	return nil
}

// Into 把 Section 设置中的某个键解码进 out。
//
// 文件里没有这个键时 out 保持原样，功能便保留初始默认值。若传入零值，就会被当成
// 用户的选择（开关全关、时长全零），从而把该功能对所有人关掉。
// 放在文件格式旁边，让各 Section 用同一方式处理缺失键。
func Into(values map[string]json.RawMessage, key string, out any) error {
	raw, present := values[key]
	if !present {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}
