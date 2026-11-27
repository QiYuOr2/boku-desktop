package panel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
)

// PanelDef 是一个面板及其展示的指标。定义由 main.go 的装配从
// internal/panel/deepseek 等包构建，因此本包从不点名任何厂商。
type PanelDef struct {
	Panel   domain.Panel
	Metrics []domain.Metric
	// OptionalMetricKeys 声明可缺席的额外指标；Panel.MetricKeys 仍是每个快照必有的集合。
	OptionalMetricKeys []string
	// UsageURL 是 provider 保存本面板所读账户的完整地址。装配未指定时留空，
	// 由 provider 自身的描述符决定。
	UsageURL string
	// Optional 标记用户可在设置窗口关闭的面板：数据不属于 Boku、或读取用户自有
	// 内容的面板。非可选面板没有开关，设置窗口也不能提供。
	Optional bool
}

// Panels 读取 provider 驱动的面板：保留每个面板最后一次良好快照，把并发刷新
// 合并为一次请求，且绝不用失败覆盖良好快照。
//
// 界面在绘制时调用 View，goroutine 调用 Refresh；两者都可从任意 goroutine、
// 任意时刻安全调用。
type Panels struct {
	registry domain.ProviderRegistry
	secrets  domain.SecretStore
	// section 持有可选面板的选择，saver 是写回时经过的存储。saver 为 nil 表示
	// 装配没有设置文件：开关仅在本次运行内有效。
	section *Section
	saver   StoreSettings
	// sources 是按 provider id 索引的、凭据并非来自存储的 provider。此处列出的
	// provider 只向其 source 询问，绝不询问存储。该 map 在 NewPanels 中一次性
	// 填充，此后只读。
	sources map[string]domain.CredentialSource
	now     func() time.Time

	mu       sync.Mutex
	defs     []PanelDef
	byID     map[string]PanelDef
	cache    map[string]cached
	lastErr  map[string]error
	inflight map[string]*inflight
	// choices 是按 id 索引的、用户关闭的面板，其值来自装配时读取以及 SetPanelOn
	// 的改动。没有条目的面板为开启。它每帧都被读取，因此不在那里读文件。
	choices map[string]bool
}

type cached struct {
	snapshot  domain.Snapshot
	fetchedAt time.Time
}

// inflight 是一次其他调用者要等待的刷新：同一面板不能同时两次询问其 provider。
type inflight struct {
	done chan struct{}
}

// View 是界面绘制一个面板所需的全部内容，从已在内存中的数据读取：
// 构建一帧从不接触网络或磁盘。
type View struct {
	Panel   domain.Panel
	Metrics []domain.Metric
	// UsageURL 是 provider 保存该账户的完整地址，provider 未指定该页面时为空。
	// 它在添加面板时解析一次，因此绘制一帧时不需查找。
	UsageURL string
	// Snapshot 是最后一次良好快照；HasSnapshot 表示是否存在。刷新失败时保留它并
	// 标记为过期，而不是显示 0。
	Snapshot    domain.Snapshot
	HasSnapshot bool
	FetchedAt   time.Time
	// Stale 表示屏幕上的数字并非最新：早于面板的间隔，或上次刷新尝试失败。
	Stale bool
	// Loading 在本面板正在刷新时为 true。
	Loading bool
	// Err 是上次尝试的失败；其类型决定卡片提供什么：缺少凭据引导去配置，
	// 其它引导重试。
	Err error
}

// NeedsCredentials 报告该面板是否因无人存储它所需的凭据而失败。
func (v View) NeedsCredentials() bool {
	return errors.Is(v.Err, domain.ErrCredentialMissing)
}

// NewPanels 校验定义并返回服务。定义有误时失败——未知 provider、面板未列的指标、
// provider 不容忍的刷新间隔——因为那些是装配错误，不是之后才在卡片里发现的东西。
//
// 对凭据不归 Boku 保管的 provider，按 provider 注册一个 source：Codex 面板读取
// Codex CLI 已完成的登录（security 设计 §9）。source 可选，没有 source 的 provider
// 一律向存储询问。
//
// section 持有可选面板的选择，saver 是写回时经过的存储；没有设置文件的装配中
// 两者都可为 nil，此时开关仅在本次运行内有效。
func NewPanels(registry domain.ProviderRegistry, secrets domain.SecretStore, section *Section, saver StoreSettings, defs []PanelDef, sources ...domain.CredentialSource) (*Panels, error) {
	if registry == nil {
		return nil, fmt.Errorf("%w: no provider registry", domain.ErrInvalidArgument)
	}
	if secrets == nil {
		return nil, fmt.Errorf("%w: no secret store", domain.ErrInvalidArgument)
	}
	if section == nil {
		section = NewSection()
	}
	panels := &Panels{
		registry: registry,
		secrets:  secrets,
		section:  section,
		saver:    saver,
		sources:  make(map[string]domain.CredentialSource, len(sources)),
		now:      func() time.Time { return time.Now().UTC() },
		byID:     make(map[string]PanelDef, len(defs)),
		cache:    make(map[string]cached),
		lastErr:  make(map[string]error),
		inflight: make(map[string]*inflight),
		choices:  make(map[string]bool),
	}
	for _, source := range sources {
		if source == nil || strings.TrimSpace(source.ProviderID()) == "" {
			return nil, fmt.Errorf("%w: a credential source names no provider", domain.ErrInvalidArgument)
		}
		if _, exists := panels.sources[source.ProviderID()]; exists {
			return nil, fmt.Errorf("%w: two credential sources for provider %s", domain.ErrAlreadyExists, source.ProviderID())
		}
		panels.sources[source.ProviderID()] = source
	}
	for _, def := range defs {
		if err := panels.add(def); err != nil {
			return nil, err
		}
	}
	return panels, nil
}

func (p *Panels) add(def PanelDef) error {
	if err := def.Panel.Validate(); err != nil {
		return err
	}
	if _, exists := p.byID[def.Panel.ID]; exists {
		return fmt.Errorf("%w: panel %s is defined twice", domain.ErrAlreadyExists, def.Panel.ID)
	}
	provider, err := p.registry.Provider(def.Panel.ProviderID)
	if err != nil {
		return err
	}
	if _, err := p.registry.Adapter(def.Panel.ProviderID); err != nil {
		return err
	}
	byKey := make(map[string]domain.Metric, len(def.Metrics))
	for _, metric := range def.Metrics {
		if err := metric.Validate(); err != nil {
			return err
		}
		if _, exists := byKey[metric.Key]; exists {
			return fmt.Errorf("%w: panel %s defines metric %s twice", domain.ErrAlreadyExists, def.Panel.ID, metric.Key)
		}
		byKey[metric.Key] = metric
	}
	for _, key := range def.Panel.MetricKeys {
		if _, ok := byKey[key]; !ok {
			return fmt.Errorf("%w: panel %s lists metric %s, which it does not define", domain.ErrInvalidArgument, def.Panel.ID, key)
		}
	}
	listed := make(map[string]bool, len(byKey))
	for _, key := range def.Panel.MetricKeys {
		listed[key] = true
	}
	for _, key := range def.OptionalMetricKeys {
		if _, ok := byKey[key]; !ok || listed[key] {
			return fmt.Errorf("%w: panel %s has an undefined or repeated optional metric %s", domain.ErrInvalidArgument, def.Panel.ID, key)
		}
		listed[key] = true
	}
	if len(listed) != len(byKey) {
		return fmt.Errorf("%w: panel %s defines %d metrics and lists %d", domain.ErrInvalidArgument, def.Panel.ID, len(byKey), len(def.Panel.MetricKeys))
	}
	if every := def.Panel.RefreshEvery; every > 0 {
		if floor := int(provider.Descriptor().MinRefreshInterval().Seconds()); every < floor {
			return fmt.Errorf("%w: panel %s refreshes every %ds, and provider %s tolerates no less than %ds",
				domain.ErrInvalidArgument, def.Panel.ID, every, def.Panel.ProviderID, floor)
		}
	}
	// 面板标题打开的链接由 provider 命名：装配可以写下一个，否则询问描述符，
	// 让地址留在拥有它的 provider 旁边。
	if strings.TrimSpace(def.UsageURL) == "" {
		def.UsageURL = provider.Descriptor().UsageURL
	}
	p.defs = append(p.defs, def)
	p.byID[def.Panel.ID] = def
	return nil
}

// List 返回已启用的面板，按展示顺序。被用户关闭的面板完全不列出：既不绘制，
// 也不会有任何东西去取它——对于凭据是他人登录的面板，这也意味着不会读取登录文件。
func (p *Panels) List() []domain.Panel {
	p.mu.Lock()
	defer p.mu.Unlock()
	panels := make([]domain.Panel, 0, len(p.defs))
	for _, def := range p.defs {
		if p.onLocked(def) {
			panels = append(panels, def.Panel)
		}
	}
	sort.SliceStable(panels, func(i, j int) bool {
		if panels[i].SortOrder != panels[j].SortOrder {
			return panels[i].SortOrder < panels[j].SortOrder
		}
		return panels[i].ID < panels[j].ID
	})
	return panels
}

// onLocked 报告面板是否展示：定义须表示如此，且用户未将其关闭。
func (p *Panels) onLocked(def PanelDef) bool {
	if !def.Panel.Enabled {
		return false
	}
	on, chosen := p.choices[def.Panel.ID]
	return !chosen || on
}

// OptionalPanels 返回设置窗口为其提供开关的面板：装配标记为可选的那些，
// 无论是否开启，按展示顺序。
func (p *Panels) OptionalPanels() []OptionalPanel {
	p.mu.Lock()
	defer p.mu.Unlock()
	panels := make([]OptionalPanel, 0, len(p.defs))
	for _, def := range p.defs {
		if !def.Optional {
			continue
		}
		panels = append(panels, OptionalPanel{
			Panel: def.Panel,
			On:    p.onLocked(def),
		})
	}
	sort.SliceStable(panels, func(i, j int) bool {
		if panels[i].Panel.SortOrder != panels[j].Panel.SortOrder {
			return panels[i].Panel.SortOrder < panels[j].Panel.SortOrder
		}
		return panels[i].Panel.ID < panels[j].Panel.ID
	})
	return panels
}

// OptionalPanel 是一个用户可关闭的面板及其状态。
type OptionalPanel struct {
	Panel domain.Panel
	On    bool
}

// LoadChoices 读取用户关闭的面板。由装配调用一次：一帧不能读文件，而选择仅通过
// SetPanelOn 变化，那里会同时记在内存与文件中。
//
// 存储读取失败不应导致启动失败：面板全部开启，正如从未关闭过任何面板的用户。
func (p *Panels) LoadChoices(ctx context.Context) error {
	if p.saver != nil {
		if err := p.saver.Load(ctx); err != nil {
			return err
		}
	}
	choices := p.section.Choices()
	if choices == nil {
		choices = make(map[string]bool)
	}
	p.mu.Lock()
	p.choices = choices
	p.mu.Unlock()
	return nil
}

// SetPanelOn 开启或关闭一个可选面板并存储该选择。
//
// 改动经由存储的 Update 而非自行 Load 再 Save：文件持有所有功能的设置，
// 在此读出又写回会把期间其他功能改动的内容恢复回去。Update 把读、改、写作为
// 一步完成，因此写入的只是本面板的选择，别无他物。存储写失败会被报告，
// 而开关已在本次运行中生效。
func (p *Panels) SetPanelOn(ctx context.Context, panelID string, on bool) error {
	p.mu.Lock()
	def, known := p.byID[panelID]
	p.mu.Unlock()
	if !known {
		return fmt.Errorf("%w: panel %s", domain.ErrNotFound, panelID)
	}
	if !def.Optional {
		return fmt.Errorf("%w: panel %s cannot be turned off", domain.ErrInvalidArgument, panelID)
	}

	var failed error
	if p.saver != nil {
		failed = p.saver.Update(ctx, sectionName, func() error {
			p.section.Set(panelID, on)
			return nil
		})
	} else {
		p.section.Set(panelID, on)
	}

	p.mu.Lock()
	if p.choices == nil {
		p.choices = make(map[string]bool)
	}
	p.choices[panelID] = on
	p.mu.Unlock()
	return failed
}

// PanelOn 按 id 报告某个面板是否开启。
func (p *Panels) PanelOn(panelID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	def, known := p.byID[panelID]
	if !known {
		return false
	}
	return p.onLocked(def)
}

// Metrics 返回某面板的指标，按展示顺序。
func (p *Panels) Metrics(panelID string) []domain.Metric {
	p.mu.Lock()
	defer p.mu.Unlock()
	def, ok := p.byID[panelID]
	if !ok {
		return nil
	}
	return append([]domain.Metric(nil), def.Metrics...)
}

// Descriptor 返回 provider 的名称与所需项，使界面能在不知道具体 provider 的情况下
// 提供配置。
func (p *Panels) Descriptor(providerID string) (domain.ProviderDescriptor, error) {
	provider, err := p.registry.Provider(providerID)
	if err != nil {
		return domain.ProviderDescriptor{}, err
	}
	return provider.Descriptor(), nil
}

// View 返回此刻已知的某面板信息。
func (p *Panels) View(panelID string) View {
	p.mu.Lock()
	defer p.mu.Unlock()
	def, ok := p.byID[panelID]
	if !ok {
		return View{}
	}
	view := View{
		Panel:    def.Panel,
		Metrics:  append([]domain.Metric(nil), def.Metrics...),
		UsageURL: def.UsageURL,
	}
	if entry, ok := p.cache[panelID]; ok {
		view.Snapshot = entry.snapshot
		view.HasSnapshot = true
		view.FetchedAt = entry.fetchedAt
		view.Stale = p.isStale(def, entry) || p.lastErr[panelID] != nil
	}
	view.Loading = p.inflight[panelID] != nil
	view.Err = p.lastErr[panelID]
	return view
}

// NeedsRefresh 报告面板快照是否缺失、过期或上次尝试失败。最新快照意味着不会再
// 询问 provider，被用户关闭的面板从不询问。
func (p *Panels) NeedsRefresh(panelID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	def, ok := p.byID[panelID]
	if !ok || !p.onLocked(def) {
		return false
	}
	if _, failed := p.lastErr[panelID]; failed {
		return true
	}
	entry, cached := p.cache[panelID]
	if !cached {
		return true
	}
	return p.isStale(def, entry)
}

// Refresh 无论快照状态如何都取数一个面板，用户请求刷新时界面即调用它。
//
// 同一面板的并发调用合并为一次请求；失败保留最后良好快照并记住，直到某次尝试成功。
// 伴随错误返回的快照是上一次的良好快照，或空。
//
// 被用户关闭的面板完全不会被询问，即使有东西调用到这里：关闭即无请求，
// 而对于凭据是他人登录的面板，这意味着不读取登录文件。
func (p *Panels) Refresh(ctx context.Context, panelID string) (domain.Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	def, known := p.byID[panelID]
	running := p.inflight[panelID]
	on := known && p.onLocked(def)
	p.mu.Unlock()
	if !known || !on {
		return domain.Snapshot{}, fmt.Errorf("%w: panel %s", domain.ErrNotFound, panelID)
	}

	if running != nil {
		select {
		case <-running.done:
		case <-ctx.Done():
			return domain.Snapshot{}, fmt.Errorf("%w: waiting for the refresh of panel %s", domain.ErrProviderTimeout, panelID)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.cache[panelID].snapshot, p.lastErr[panelID]
	}

	call := &inflight{done: make(chan struct{})}
	p.mu.Lock()
	if p.inflight[panelID] != nil {
		// 另一个调用者在两次加锁之间启动了刷新：让它执行，并等待它。
		existing := p.inflight[panelID]
		p.mu.Unlock()
		select {
		case <-existing.done:
		case <-ctx.Done():
			return domain.Snapshot{}, fmt.Errorf("%w: waiting for the refresh of panel %s", domain.ErrProviderTimeout, panelID)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.cache[panelID].snapshot, p.lastErr[panelID]
	}
	p.inflight[panelID] = call
	p.mu.Unlock()

	snapshot, err := p.fetch(ctx, def)

	p.mu.Lock()
	delete(p.inflight, panelID)
	result := snapshot
	if err != nil {
		p.lastErr[panelID] = err
		result = p.cache[panelID].snapshot
	} else {
		p.cache[panelID] = cached{snapshot: snapshot, fetchedAt: p.now()}
		delete(p.lastErr, panelID)
	}
	close(call.done)
	p.mu.Unlock()
	return result, err
}

// RefreshIfNeeded 仅在必要时取数：尚无快照、快照早于面板刷新间隔，或上次尝试失败。
// 它报告是否询问了 provider。
func (p *Panels) RefreshIfNeeded(ctx context.Context, panelID string) (domain.Snapshot, bool, error) {
	p.mu.Lock()
	def, known := p.byID[panelID]
	on := known && p.onLocked(def)
	running := p.inflight[panelID] != nil
	entry, cached := p.cache[panelID]
	_, failed := p.lastErr[panelID]
	fresh := false
	if known && cached && !failed {
		fresh = !p.isStale(def, entry)
	}
	p.mu.Unlock()

	if !known || !on {
		return domain.Snapshot{}, false, fmt.Errorf("%w: panel %s", domain.ErrNotFound, panelID)
	}
	if fresh && !running {
		return entry.snapshot, false, nil
	}
	snapshot, err := p.Refresh(ctx, panelID)
	return snapshot, true, err
}

// TestProvider 校验凭据但不存储：取数该 provider 的面板一次并映射结果。
// 设置窗口用它告诉用户在保存密钥前它是否可用。
func (p *Panels) TestProvider(ctx context.Context, providerID string, secrets map[string]string) error {
	p.mu.Lock()
	var def PanelDef
	found := false
	for _, candidate := range p.defs {
		if candidate.Panel.ProviderID == providerID {
			def, found = candidate, true
			break
		}
	}
	p.mu.Unlock()
	if !found {
		return fmt.Errorf("%w: no panel uses provider %s", domain.ErrProviderNotFound, providerID)
	}
	provider, err := p.registry.Provider(providerID)
	if err != nil {
		return err
	}
	adapter, err := p.registry.Adapter(providerID)
	if err != nil {
		return err
	}
	// 缺失的候选凭据在此处就答复，使 provider 只在确实持有凭据时被询问。
	for _, key := range provider.Descriptor().CredentialKeys {
		if strings.TrimSpace(secrets[key]) == "" {
			return fmt.Errorf("%w: %s is empty", domain.ErrCredentialMissing, domain.CredentialKey(providerID, key))
		}
	}
	creds := domain.Credentials{
		ProviderID: providerID,
		Secrets:    secrets,
		Settings:   map[string]string{},
	}
	result, err := provider.Fetch(ctx, creds)
	if err != nil {
		return err
	}
	if result.ProviderID == "" {
		result.ProviderID = providerID
	}
	if _, err := adapter.Normalize(result, def.Panel); err != nil {
		return err
	}
	return nil
}

// fetch 询问 provider 并映射结果。
func (p *Panels) fetch(ctx context.Context, def PanelDef) (domain.Snapshot, error) {
	providerID := def.Panel.ProviderID
	provider, err := p.registry.Provider(providerID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	adapter, err := p.registry.Adapter(providerID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	creds, err := p.credentials(ctx, provider.Descriptor())
	if err != nil {
		return domain.Snapshot{}, err
	}
	result, err := provider.Fetch(ctx, creds)
	if errors.Is(err, domain.ErrProviderUnauthorized) {
		if source, ok := p.sources[providerID].(domain.CredentialRefresher); ok {
			refreshed, refreshErr := source.RefreshCredentials(ctx)
			if refreshErr != nil {
				return domain.Snapshot{}, refreshErr
			}
			result, err = provider.Fetch(ctx, refreshed)
		}
	}
	if err != nil {
		return domain.Snapshot{}, err
	}
	if result.ProviderID == "" {
		result.ProviderID = providerID
	}
	snapshot, err := adapter.Normalize(result, def.Panel)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if err := checkSnapshot(def, snapshot); err != nil {
		return domain.Snapshot{}, err
	}
	return snapshot, nil
}

// StartCredentialLogin 启动 provider 凭据来源的登录，用于凭据不归 Boku 保管的
// provider：用户登录该工具前面板无可读取，这里省去他们去找终端。
//
// 没有 source、或 source 没有自身登录的 provider 会如实报错：界面只在 source
// 有登录时才提供该操作。
func (p *Panels) StartCredentialLogin(providerID string) error {
	source, ok := p.sources[providerID]
	if !ok {
		return fmt.Errorf("%w: provider %s has no credential source", domain.ErrProviderNotFound, providerID)
	}
	starter, ok := source.(domain.LoginStarter)
	if !ok {
		return fmt.Errorf("%w: the credential source of %s has no login to start", domain.ErrInvalidArgument, providerID)
	}
	return starter.StartLogin()
}

// credentials 收集 provider 所需的凭据。缺失时在调用 provider 前即返回
// ErrCredentialMissing：用不存在的密钥去请求毫无意义。
//
// 有自身 source 的 provider 向该 source 询问：其凭据不在存储中——且不得复制进去——
// 因此完全不查存储。
func (p *Panels) credentials(ctx context.Context, descriptor domain.ProviderDescriptor) (domain.Credentials, error) {
	if source, ok := p.sources[descriptor.ID]; ok {
		return source.Credentials(ctx)
	}
	creds := domain.Credentials{
		ProviderID: descriptor.ID,
		Secrets:    make(map[string]string, len(descriptor.CredentialKeys)),
		Settings:   map[string]string{},
	}
	for _, key := range descriptor.CredentialKeys {
		ref := domain.CredentialKey(descriptor.ID, key)
		secret, err := p.secrets.Get(ctx, ref)
		if errors.Is(err, domain.ErrSecretNotFound) {
			return creds, fmt.Errorf("%w: %s is not configured", domain.ErrCredentialMissing, ref)
		}
		if err != nil {
			return creds, err
		}
		value := strings.TrimSpace(secret.Text())
		if value == "" {
			return creds, fmt.Errorf("%w: %s is empty", domain.ErrCredentialMissing, ref)
		}
		creds.Secrets[key] = value
	}
	return creds, nil
}

// checkSnapshot 让每个 adapter 遵守面板契约：面板的指标、按面板顺序、
// 各自声明的值类型。
func checkSnapshot(def PanelDef, snapshot domain.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	metrics := make(map[string]domain.Metric, len(def.Metrics))
	for _, metric := range def.Metrics {
		metrics[metric.Key] = metric
	}
	seen := make(map[string]bool, len(snapshot.Observations))
	for _, observation := range snapshot.Observations {
		metric, ok := metrics[observation.MetricKey]
		if !ok {
			return fmt.Errorf("%w: the snapshot of panel %s has metric %s, which the panel does not define",
				domain.ErrInvalidMetricValue, def.Panel.ID, observation.MetricKey)
		}
		if !observation.Value.Matches(metric.ValueType) {
			return fmt.Errorf("%w: observation of %s does not match its value type %s",
				domain.ErrInvalidMetricValue, observation.MetricKey, metric.ValueType)
		}
		// 只有金额有币种可解析：百分比、时长和文本不是金额，向它们索取币种
		// 会拒绝每一个。
		if metric.Unit == domain.UnitCurrency {
			if _, err := snapshot.MetricCurrency(metric); err != nil {
				return err
			}
		}
		seen[observation.MetricKey] = true
	}
	for _, key := range def.Panel.MetricKeys {
		if !seen[key] {
			return fmt.Errorf("%w: the snapshot of panel %s has no value for metric %s",
				domain.ErrInvalidMetricValue, def.Panel.ID, key)
		}
	}
	return nil
}

// isStale 报告快照是否早于面板间隔。调用方须持有锁。
func (p *Panels) isStale(def PanelDef, entry cached) bool {
	if def.Panel.RefreshEvery <= 0 {
		return false
	}
	return p.now().Sub(entry.fetchedAt) > time.Duration(def.Panel.RefreshEvery)*time.Second
}
