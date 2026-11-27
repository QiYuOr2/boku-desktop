package ui

import (
	contractdeepseek "boku/internal/contract/deepseek"
	"boku/internal/credential"
	"boku/internal/domain"
	"boku/internal/presentation"
	"boku/internal/translate"
	"context"
	"errors"
	"github.com/egoist/mygo/ui"
	"log/slog"
	"strings"
	"time"
)

const (
	StatusNone = iota
	StatusOK
	StatusError
)

type CardStatus struct {
	Status int
	Text   string
}

type ConnectForm struct {
	// Keys 只存用户为每个翻译器输入的内容；已保存的凭据不会回填到输入框。
	Keys map[string]string
	// HasCredential 表示各提供商是否已有凭据。
	HasCredential map[string]bool
	// NewName、NewURL、NewModel 是正在添加的服务，不是凭据：它们明文存放在设置文件里，添加成功后即清空。
	NewName  string
	NewURL   string
	NewModel string
	// Busy 是正在测试或保存的卡片 ID，空闲为空。
	Busy string
	// BusyNote 是该卡片当前的忙碌说明（连接、保存、删除三种操作中只有第一种是「连接」）。
	BusyNote string
	// DeleteArmed 是已请求删除但尚未确认的卡片。
	DeleteArmed string
	// Status 是各卡片最近一次操作的结果。
	Status map[string]CardStatus
	// InputCard 是当前显示凭据输入框的卡片，Input 是其输入内容。
	//
	// 输入框需要跨越帧持续存在的字符串（框架按地址绑定），而卡片是在循环里绘制的、同一时刻只有一个输入框，因此输入暂存在这里，切换卡片时归档到对应卡片下。
	InputCard string
	Input     string
}

type TranslateCard = presentation.Connection

func (s *State) ApiKeySection(c *ui.Context, head *ui.Handle) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, presentation.GroupGap, func() {
			presentation.SectionHeadSmall(c, head, s.keyMark, presentation.ColorTextMuted, "API Key", "")

			// 只列出需要密钥的服务：本区是密钥表单，每项服务占单层面板内一行；无需密钥的服务在翻译区选择。
			cards := KeyedTranslateCards(s.TranslateCards())
			if len(cards) > 0 {
				presentation.SettingsPanel(c, func() {
					for index, card := range cards {
						if index > 0 {
							presentation.SettingsDivider(c)
						}
						s.ApiKeyCard(c, card)
					}
				})
			}
			// 空白表单作为独立同级面板置于底部，避免大框套小框。
			s.NewCustomServiceCard(c)
		})
	})
}

func KeyedTranslateCards(cards []TranslateCard) []TranslateCard {
	keyed := make([]TranslateCard, 0, len(cards))
	own := make([]TranslateCard, 0, len(cards))
	for _, card := range cards {
		if translate.IsCustomService(card.ID) {
			own = append(own, card)
			continue
		}
		if card.NeedsCredential {
			keyed = append(keyed, card)
		}
	}
	return append(keyed, own...)
}

func (s *State) ApiKeyCard(c *ui.Context, card TranslateCard) {
	row := ui.Column(c).FillWidth().
		Padding(presentation.SettingRowPadY, presentation.SettingRowPadX).
		Gap(presentation.RowGap)
	hot := row.Hovered() || s.Form.DeleteArmed == card.ID
	row.Children(func() {
		s.ApiKeyCardHead(c, card, hot)
		s.CredentialRow(c, card)
		s.CardStatusLine(c, card)
		s.ApiKeyCardNote(c, card)
	})
}

func (s *State) ApiKeyCardHead(c *ui.Context, card TranslateCard, hot bool) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
		ui.Text(c, card.DisplayName).
			FontSize(presentation.SizeCardName).
			FontWeight(600).
			TextColor(presentation.ColorText).
			SingleLine()
		ConfiguredBadge(c, card.Configured)
		ui.Spacer(c)
		if !CardCanBeRemoved(card, s.Form.HasCredential[card.ID]) {
			return
		}
		actions := ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(float32(4))
		if !hot {
			actions = actions.Opacity(0).PassThrough()
		}
		actions.Children(func() {
			s.DeleteActions(c, card)
		})
	})
}

func CardCanBeRemoved(card TranslateCard, HasCredential bool) bool {
	return translate.IsCustomService(card.ID) || HasCredential
}

func (s *State) NewCustomServiceCard(c *ui.Context) {
	presentation.SettingsPanel(c, func() {
		presentation.SettingsRow(c, func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
				ui.Text(c, CustomServiceCardTitle).
					FontSize(presentation.SizeCardName).
					FontWeight(600).
					TextColor(presentation.ColorText).
					SingleLine()
				presentation.KindBadge(c, presentation.ColorStoneTile, presentation.ColorStoneInk, "OpenAI 兼容")
			})
			presentation.Stacked(c, presentation.FieldGap, func() {
				s.NewServiceNameField(c)
				s.NewServiceURLField(c)
				s.NewServiceModelField(c)
				s.NewServiceRow(c)
			})
			s.CardStatusLine(c, TranslateCard{ID: NewCustomServiceID})
		})
	})
}

const CustomServiceCardTitle = "添加自定义服务"

const NewCustomServiceID = "custom-new"

func (s *State) DeleteActions(c *ui.Context, card TranslateCard) {
	if s.Form.DeleteArmed != card.ID {
		if action := presentation.TextAction(c, "删除", presentation.ColorDanger); action.Clicked() {
			s.ArmDelete(card.ID)
		}
		return
	}
	if action := presentation.TextAction(c, "确认删除", presentation.ColorDanger); action.Clicked() {
		s.DeleteCredential(card)
	}
	if action := presentation.TextAction(c, "取消", presentation.ColorTextMuted); action.Clicked() {
		s.CancelDelete()
	}
}

func (s *State) CredentialRow(c *ui.Context, card TranslateCard) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
		s.CredentialField(c, card)
		if button := presentation.CardButton(c, "测试", presentation.ButtonSecondary); button.Clicked() {
			s.TestCredential(card)
		}
		label := "保存"
		if card.ID == customTranslatorID {
			label = "添加"
		}
		if button := presentation.CardButton(c, label, presentation.ButtonPrimary); button.Clicked() {
			s.SaveCredential(card)
		}
	})
}

func (s *State) ApiKeyCardNote(c *ui.Context, card TranslateCard) {
	_ = card
}

func ConfiguredBadge(c *ui.Context, Configured bool) {
	tile, ink, label := presentation.ColorStoneTile, presentation.ColorStoneInk, "未配置"
	if Configured {
		tile, ink, label = presentation.ColorEmeraldTile, presentation.ColorEmeraldInk, "已配置"
	}
	presentation.KindBadge(c, tile, ink, label)
}

func (s *State) CredentialField(c *ui.Context, card TranslateCard) {
	s.ShowCardInput(card.ID)
	field := ui.TextInput(c, &s.Form.Input)
	field.Password().Placeholder(CredentialHint(card)).SingleLine()
	field.Label(card.DisplayName + " " + CredentialLabel(card.CredentialKey))
	field.Disabled(s.Form.Busy != "")
	presentation.FieldFrame(c, field).Grow(1).Font("monospace")
}

func (s *State) ShowCardInput(ID string) {
	if s.Form.InputCard == ID {
		return
	}
	if s.Form.InputCard != "" {
		if s.Form.Keys == nil {
			s.Form.Keys = map[string]string{}
		}
		s.Form.Keys[s.Form.InputCard] = s.Form.Input
	}
	s.Form.InputCard = ID
	s.Form.Input = ""
	if s.Form.Keys != nil {
		s.Form.Input = s.Form.Keys[ID]
	}
}

func (s *State) KeyValue(ID string) string { return s.CardInput(ID) }

func (s *State) ClearCardInput(ID string) {
	if s.Form.Keys != nil {
		delete(s.Form.Keys, ID)
	}
	if s.Form.InputCard == ID {
		s.Form.Input = ""
	}
}

func (s *State) NewServiceNameField(c *ui.Context) {
	field := ui.TextInput(c, &s.Form.NewName)
	field.Placeholder("名称，如本地 Ollama").Label("名称").SingleLine()
	field.Disabled(s.Form.Busy != "")
	presentation.FieldFrame(c, field).FillWidth()
}

func (s *State) NewServiceURLField(c *ui.Context) {
	field := ui.TextInput(c, &s.Form.NewURL)
	field.Placeholder("Base URL  https://api.example.com/v1").Label("Base URL").SingleLine()
	field.Disabled(s.Form.Busy != "")
	presentation.FieldFrame(c, field).FillWidth().Font("monospace")
}

func (s *State) NewServiceModelField(c *ui.Context) {
	field := ui.TextInput(c, &s.Form.NewModel)
	field.Placeholder("模型名称，如 gpt-4o-mini").Label("模型名称").SingleLine()
	field.Disabled(s.Form.Busy != "")
	presentation.FieldFrame(c, field).FillWidth().Font("monospace")
}

func (s *State) NewServiceRow(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
		s.ShowCardInput(NewCustomServiceID)
		field := ui.TextInput(c, &s.Form.Input)
		field.Password().Placeholder("API Key（可选）").SingleLine()
		field.Label("自定义服务 API Key")
		field.Disabled(s.Form.Busy != "")
		presentation.FieldFrame(c, field).Grow(1).Font("monospace")

		if button := presentation.CardButton(c, "添加", presentation.ButtonPrimary); button.Clicked() {
			s.AddCustomService()
		}
	})
}

func (s *State) CardStatusLine(c *ui.Context, card TranslateCard) {
	if s.Form.Busy == card.ID {
		Text := s.Form.BusyNote
		if Text == "" {
			Text = "正在连接 " + card.DisplayName + "…"
		}
		ui.Row(c).AlignItems(ui.Center).Gap(float32(8)).Children(func() {
			ui.Spinner(c)
			ui.Text(c, Text).
				FontSize(presentation.SizeSmall).
				TextColor(presentation.ColorTextMuted)
		})
		return
	}
	state, ok := s.Form.Status[card.ID]
	if !ok {
		return
	}
	switch state.Status {
	case StatusOK:
		presentation.Notice(c, presentation.NoteSuccess, state.Text)
	case StatusError:
		presentation.Notice(c, presentation.NoteDanger, state.Text)
	}
}

func (s *State) ArmDelete(ID string) {
	s.Form.DeleteArmed = ID
	s.host.Refresh()
}

func (s *State) CancelDelete() {
	s.Form.DeleteArmed = ""
	s.host.Refresh()
}

func (s *State) DeleteCredential(card TranslateCard) {
	if s.Form.Busy != "" || s.service == nil {
		return
	}
	ownService := translate.IsCustomService(card.ID)
	if !ownService && s.secrets == nil {
		return
	}
	secrets, window := s.secrets, s.host
	service := s.service
	ID, key := card.ID, card.CredentialKey
	useFreeAfterDeletion := ShouldUseFreeProviderAfterCredentialDeletion(ID, s.TranslateSettingsOr().ProviderOr())
	s.Form.Busy, s.Form.DeleteArmed = card.ID, ""
	slog.Info("translator configuration deletion requested", "event", "translator_configuration_deletion_requested", "provider", card.ID, "customService", ownService)
	if ownService {
		s.Form.BusyNote = "正在删除这个服务…"
	} else {
		s.Form.BusyNote = "正在删除 " + CredentialLabel(key) + "…"
	}
	s.SetCardStatus(ID, StatusNone, "")

	go func() {
		ctx := context.Background()
		var err error
		if ownService {
			// 顺序与添加相反：先注销服务，避免还有地方请求即将消失的地址，之后再删除密钥。
			err = service.RemoveCustomTranslator(ctx, ID)
			if err == nil {
				err = ForgetCredential(ctx, secrets, ID, key, s.Form.HasCredential[ID])
			}
			if err == nil {
				// 翻译器也要一并注销：卡片来自注册表，若只从设置移除而留在注册表，会残留一张没有地址的卡片。
				if syncErr := s.UnregisterOwnServices(); syncErr != nil {
					slog.Warn("the registry could not be brought in step", "error", syncErr)
				}
			}
		} else {
			err = secrets.Delete(ctx, ID, key)
		}
		if err == nil && useFreeAfterDeletion {
			err = service.SetProvider(ctx, translate.DefaultProvider)
		}
		if window.RunOnMain == nil {
			// 无窗口：该 state 仅在测试构建视图时存在，无需回传结果。
			return
		}
		if err != nil {
			slog.Warn("translator configuration deletion failed", "event", "translator_configuration_deletion_failed", "provider", ID, "error", err)
		} else {
			slog.Info("translator configuration deleted", "event", "translator_configuration_deleted", "provider", ID, "customService", ownService)
		}
		window.OnMain(func() { s.CredentialForgotten(ID, useFreeAfterDeletion, err) })
	}()
}

func ForgetCredential(ctx context.Context, secrets *credential.Secrets, ID, key string, stored bool) error {
	if secrets == nil || !stored {
		return nil
	}
	err := secrets.Delete(ctx, ID, key)
	if errors.Is(err, domain.ErrSecretNotFound) {
		return nil
	}
	return err
}

func (s *State) CredentialForgotten(ID string, usedFreeProvider bool, err error) {
	s.Form.Busy = ""
	if err != nil {
		s.SetCardStatus(ID, StatusError, presentation.FriendlyError(err))
		return
	}
	s.ReloadCredentialsAfterSave()
	message := "已删除。"
	if usedFreeProvider {
		message = "已删除，已切换到免费服务。"
	}
	s.SetCardStatus(ID, StatusOK, message)
}

func ShouldUseFreeProviderAfterCredentialDeletion(removedID, selectedID string) bool {
	return removedID == contractdeepseek.ProviderID && selectedID == contractdeepseek.ProviderID
}

func (s *State) TranslateCards() []TranslateCard {
	if s.service == nil {
		return nil
	}
	views, err := s.service.Providers(context.Background())
	if err != nil {
		return nil
	}
	cards := make([]TranslateCard, 0, len(views))
	for _, view := range views {
		cards = append(cards, TranslateCard{
			ID:              view.ID,
			DisplayName:     view.DisplayName,
			DocsURL:         view.DocsURL,
			Endpoint:        view.Endpoint,
			CredentialKey:   view.CredentialKey,
			NeedsCredential: view.NeedsCredential,
			Configured:      view.Configured,
			SupportsImage:   view.SupportsImage,
		})
	}
	return cards
}

func (s *State) TranslateSettings() (translate.Settings, error) {
	if s.service == nil {
		return translate.Defaults(), nil
	}
	return s.service.Settings()
}

func (s *State) TranslateSettingsOr() translate.Settings {
	settings, err := s.TranslateSettings()
	if err != nil {
		return translate.Defaults()
	}
	return settings
}

func CardEndpointNote(Endpoint string) string {
	if index := strings.Index(Endpoint, "（"); index >= 0 {
		return strings.TrimSpace(Endpoint[:index])
	}
	return strings.TrimSpace(Endpoint)
}

func (s *State) CredentialIsShown(card TranslateCard) bool {
	return s.Form.HasCredential[card.ID] && strings.TrimSpace(s.CardInput(card.ID)) == ""
}

func (s *State) CardInput(ID string) string {
	if s.Form.InputCard == ID {
		return strings.TrimSpace(s.Form.Input)
	}
	if s.Form.Keys == nil {
		return ""
	}
	return strings.TrimSpace(s.Form.Keys[ID])
}

func (s *State) LoadCredentials() {
	s.Form = ConnectForm{
		Keys:          map[string]string{},
		HasCredential: map[string]bool{},
		Status:        map[string]CardStatus{},
	}
	for _, card := range s.TranslateCards() {
		names, err := s.secrets.Names(context.Background(), card.ID)
		if err != nil {
			s.Form.Status[card.ID] = CardStatus{Status: StatusError, Text: presentation.FriendlyError(err)}
			continue
		}
		s.Form.HasCredential[card.ID] = CredentialStored(names, card)
	}
}

func CredentialStored(names []string, card TranslateCard) bool {
	want := domain.CredentialKey(card.ID, card.CredentialKey)
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func (s *State) ReloadCredentialsAfterSave() {
	status, keys := s.Form.Status, s.Form.Keys
	name, url, model := s.Form.NewName, s.Form.NewURL, s.Form.NewModel
	s.LoadCredentials()
	s.Form.Status = status
	if keys != nil {
		s.Form.Keys = keys
	}
	s.Form.NewName, s.Form.NewURL, s.Form.NewModel = name, url, model
}

func (s *State) TestCredential(card TranslateCard) {
	if s.Form.Busy != "" || s.service == nil {
		return
	}
	if s.CredentialIsShown(card) {
		s.SetCardStatus(card.ID, StatusError, "要先填入新的 "+
			CredentialLabel(card.CredentialKey)+"，再测试连接。")
		return
	}
	if card.NeedsCredential && strings.TrimSpace(s.KeyValue(card.ID)) == "" {
		s.SetCardStatus(card.ID, StatusError, "请先填入 "+CredentialLabel(card.CredentialKey)+"。")
		return
	}
	window := s.host
	service := s.service
	override := map[string]string{}
	if value := strings.TrimSpace(s.KeyValue(card.ID)); value != "" {
		override[card.CredentialKey] = value
	}
	s.Form.Busy = card.ID
	slog.Info("translator connection test requested", "event", "translator_connection_test_requested", "provider", card.ID, "hasCredentialOverride", len(override) > 0)
	s.Form.BusyNote = "正在连接 " + card.DisplayName + "…"
	s.SetCardStatus(card.ID, StatusNone, "")

	go func() {
		startedAt := time.Now()
		err := service.TestTranslator(context.Background(), card.ID, override)
		if err != nil {
			slog.Warn("translator connection test failed", "event", "translator_connection_test_failed", "provider", card.ID, "duration", time.Since(startedAt), "error", err)
		} else {
			slog.Info("translator connection test completed", "event", "translator_connection_test_completed", "provider", card.ID, "duration", time.Since(startedAt))
		}
		window.OnMain(func() {
			s.Form.Busy = ""
			if err != nil {
				s.SetCardStatus(card.ID, StatusError, presentation.FriendlyError(err))
				return
			}
			s.SetCardStatus(card.ID, StatusOK, "连接成功，可以保存了。")
		})
	}()
}

func (s *State) AddCustomService() {
	if s.Form.Busy != "" || s.service == nil {
		return
	}
	name := strings.TrimSpace(s.Form.NewName)
	Configured := translate.CustomSettings{BaseURL: s.Form.NewURL, Model: s.Form.NewModel}.Normalize()
	address, model := Configured.BaseURL, Configured.Model
	if address == "" {
		s.SetCardStatus(NewCustomServiceID, StatusError, "请先填写 Base URL。")
		s.host.Refresh()
		return
	}
	if model == "" {
		s.SetCardStatus(NewCustomServiceID, StatusError, "请填写模型名称。")
		s.host.Refresh()
		return
	}
	key := strings.TrimSpace(s.CardInput(NewCustomServiceID))
	window := s.host
	service := s.service
	secrets := s.secrets

	s.Form.Busy = NewCustomServiceID
	slog.Info("custom translator creation requested", "event", "custom_translator_creation_requested", "hasAPIKey", key != "")
	s.Form.BusyNote = "正在添加…"
	s.SetCardStatus(NewCustomServiceID, StatusNone, "")

	go func() {
		startedAt := time.Now()
		ctx := context.Background()
		ID, err := service.AddCustomTranslator(ctx, translate.CustomSettings{
			BaseURL:     address,
			DisplayName: name,
			Model:       model,
		})
		if err == nil && key != "" && secrets != nil {
			err = secrets.Set(ctx, ID, "apiKey", key)
		}
		if err == nil {
			// 新服务必须先注册翻译器才能被调用。
			if err = s.RegisterOwnServices(); err != nil {
				slog.Warn("the new service could not be registered", "error", err)
			}
		}
		// 自定义服务可能指向任意 OpenAI 兼容网关，其模型目录不可靠地声明视觉支持，因此需在密钥与翻译器就绪后用一张小图实测该 Endpoint。探测失败不撤销服务（仍可作文本翻译），能力保持为 false，并记录日志以便解释卡片为何显示仅支持文本。
		imageSupported := false
		if err == nil {
			probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			supported, probeErr := service.DetectCustomImageCapability(probeCtx, ID)
			cancel()
			if probeErr != nil {
				slog.Warn("the image capability probe did not finish", "event", "image_probe_failed", "provider", ID, "error", probeErr)
			}
			imageSupported = supported
		}
		if err != nil {
			slog.Warn("custom translator creation failed", "event", "custom_translator_creation_failed", "duration", time.Since(startedAt), "error", err)
		} else {
			slog.Info("custom translator created", "event", "custom_translator_created", "provider", ID, "imageSupported", imageSupported, "duration", time.Since(startedAt))
		}
		window.OnMain(func() {
			s.Form.Busy = ""
			if err != nil {
				s.SetCardStatus(NewCustomServiceID, StatusError, presentation.FriendlyError(err))
				return
			}
			s.Form.NewName, s.Form.NewURL, s.Form.NewModel = "", "", ""
			s.ClearCardInput(NewCustomServiceID)
			s.ReloadCredentialsAfterSave()
			if imageSupported {
				s.SetCardStatus(ID, StatusOK, "已添加，支持图片翻译。")
			} else if key != "" {
				s.SetCardStatus(ID, StatusOK, "已添加，仅支持文本翻译。")
			} else {
				s.SetCardStatus(ID, StatusOK, "已添加。")
			}
			s.SetCardStatus(NewCustomServiceID, StatusNone, "")
			if key == "" {
				s.TestCredential(CardOfID(s, ID))
			}
		})
	}()
}

func (s *State) RegisterOwnServices() error {
	if s.ownServices == nil {
		return nil
	}
	return s.ownServices()
}

func (s *State) UnregisterOwnServices() error { return s.RegisterOwnServices() }

func CardOfID(s *State, ID string) TranslateCard {
	for _, card := range s.TranslateCards() {
		if card.ID == ID {
			return card
		}
	}
	return TranslateCard{ID: ID}
}

func (s *State) SaveCredential(card TranslateCard) {
	if s.Form.Busy != "" || s.service == nil {
		return
	}
	if s.CredentialIsShown(card) {
		s.SetCardStatus(card.ID, StatusError, "要先填入新的 "+
			CredentialLabel(card.CredentialKey)+"，再保存。")
		return
	}
	key := strings.TrimSpace(s.KeyValue(card.ID))
	if card.NeedsCredential && key == "" {
		s.SetCardStatus(card.ID, StatusError, "请先填入 "+CredentialLabel(card.CredentialKey)+"。")
		return
	}
	if !card.NeedsCredential && key == "" {
		// 无需存储：可选密钥为空说明该服务不需要密钥，且已配置。
		s.SetCardStatus(card.ID, StatusOK, "这个服务不需要密钥。")
		s.host.Refresh()
		return
	}
	window := s.host
	secrets := s.secrets
	ID, CredentialKey := card.ID, card.CredentialKey
	s.Form.Busy = card.ID
	slog.Info("translator credential save requested", "event", "translator_credential_save_requested", "provider", card.ID, "credentialKey", card.CredentialKey)
	s.Form.BusyNote = "正在保存…"
	s.SetCardStatus(card.ID, StatusNone, "")

	go func() {
		err := secrets.Set(context.Background(), ID, CredentialKey, key)
		if err != nil {
			slog.Warn("translator credential save failed", "event", "translator_credential_save_failed", "provider", ID, "credentialKey", CredentialKey, "error", err)
		} else {
			slog.Info("translator credential saved", "event", "translator_credential_saved", "provider", ID, "credentialKey", CredentialKey)
		}
		window.OnMain(func() {
			s.Form.Busy = ""
			if err != nil {
				s.SetCardStatus(ID, StatusError, presentation.FriendlyError(err))
				return
			}
			s.ClearCardInput(ID)
			s.ReloadCredentialsAfterSave()
			s.SetCardStatus(ID, StatusOK, "已保存。")
			// 面板刚拿到缺失的凭据，立即刷新数据。
			s.refreshPanels()
		})
	}()
}

func (s *State) SetCardStatus(ID string, Status int, Text string) {
	if s.Form.Status == nil {
		s.Form.Status = map[string]CardStatus{}
	}
	s.Form.Status[ID] = CardStatus{Status: Status, Text: Text}
}

func (s *State) CredentialPath() string {
	if path := s.secrets.Path(); path != "" {
		return path
	}
	return "本机文件"
}

func (s *State) CredentialFile() string { return FileOf(s.CredentialPath()) }

func FileOf(path string) string {
	path = strings.TrimSpace(path)
	if index := strings.LastIndexAny(path, `/\`); index >= 0 {
		if name := strings.TrimSpace(path[index+1:]); name != "" {
			return name
		}
	}
	return path
}

func CredentialLabel(key string) string {
	switch key {
	case "apiKey":
		return "API Key"
	case "adminKey":
		return "Admin Key"
	case "steamId":
		return "Steam ID"
	case "":
		return "凭据"
	default:
		return key
	}
}

func CredentialHint(card TranslateCard) string {
	if !card.NeedsCredential {
		return "API Key（可选）"
	}
	if card.CredentialKey == "apiKey" {
		return "sk-......"
	}
	return CredentialLabel(card.CredentialKey)
}

func DocsAddress(url string) string {
	trimmed := strings.TrimSpace(url)
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	return trimmed
}
