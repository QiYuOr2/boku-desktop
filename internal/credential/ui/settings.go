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
	// Keys is what the user typed, per translator ID, and only that. A
	// stored credential never goes into a field.
	Keys map[string]string
	// HasCredential is whether each provider has one.
	HasCredential map[string]bool
	// NewName, NewURL and NewModel are the service being added, as typed into the
	// blank card at the foot of the section. They are not credentials: they
	// live in the settings file, in the clear, and they are cleared once the
	// service is added.
	NewName  string
	NewURL   string
	NewModel string
	// Busy is the card that is being tested or saved, or empty.
	Busy string
	// BusyNote says what that card is Busy with, as the line under it reads.
	// A card does three things — connect, save, remove — and only the first
	// of them is a connection.
	BusyNote string
	// DeleteArmed is the card whose stored credential has been asked about
	// and not yet confirmed: the card that is one press away from losing it.
	DeleteArmed string
	// Status is what each card's last attempt said.
	Status map[string]CardStatus
	// InputCard is the card the credential field is showing, and Input is
	// what has been typed into it.
	//
	// The field needs one string that outlives a frame — the framework binds
	// it by address — while the cards are drawn in a loop and only one of
	// them has a field at a time. So the typing lives here, and it is filed
	// under its card as soon as another card's field is built.
	InputCard string
	Input     string
}

type TranslateCard = presentation.Connection

func (s *State) ApiKeySection(c *ui.Context, head *ui.Handle) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, float32(16), func() {
			presentation.SectionHeadSmall(c, head, s.keyMark, presentation.ColorStoneInk, "API Key", "配置各 AI 服务密钥")

			// Only the services that have a key: this section is a key form,
			// and a card here is a field to fill in. The services that need
			// nothing are chosen in the translation section instead, where the
			// user is deciding what to translate with rather than what to
			// type.
			cards := KeyedTranslateCards(s.TranslateCards())
			presentation.Stacked(c, float32(12), func() {
				for _, card := range cards {
					s.ApiKeyCard(c, card)
				}
				// The blank card is always the last one, and it is always
				// there: adding a second service is how a user who already
				// has one gets another, and a section whose only way to add is
				// to email the author is not one.
				s.NewCustomServiceCard(c)
			})
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
	ui.Column(c).FillWidth().
		Background(presentation.ColorSurface).
		Border(1, presentation.ColorBorder).
		Radius(presentation.RadiusField).
		Padding(float32(10), float32(12)).
		Children(func() {
			presentation.Stacked(c, float32(6), func() {
				s.ApiKeyCardHead(c, card)
				s.CredentialRow(c, card)
				s.CardStatusLine(c, card)
				s.ApiKeyCardNote(c, card)
			})
		})
}

func (s *State) ApiKeyCardHead(c *ui.Context, card TranslateCard) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
		ui.Text(c, card.DisplayName).
			FontSize(presentation.SizeCardName).
			FontWeight(600).
			TextColor(presentation.ColorTextSoft).
			SingleLine()
		ConfiguredBadge(c, card.Configured)
		ui.Spacer(c)
		if !CardCanBeRemoved(card, s.Form.HasCredential[card.ID]) {
			return
		}
		s.DeleteActions(c, card)
	})
}

func CardCanBeRemoved(card TranslateCard, HasCredential bool) bool {
	return translate.IsCustomService(card.ID) || HasCredential
}

func (s *State) NewCustomServiceCard(c *ui.Context) {
	ui.Column(c).FillWidth().
		Background(presentation.ColorSurface).
		Border(1, presentation.ColorBorder).
		Radius(presentation.RadiusField).
		Padding(float32(10), float32(12)).
		Children(func() {
			presentation.Stacked(c, float32(6), func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(8)).Children(func() {
					ui.Text(c, CustomServiceCardTitle).
						FontSize(presentation.SizeCardName).
						FontWeight(600).
						TextColor(presentation.ColorTextSoft).
						SingleLine()
					presentation.KindBadge(c, presentation.ColorStoneTile, presentation.ColorStoneInk, "OpenAI 兼容")
				})
				presentation.Stacked(c, float32(6), func() {
					s.NewServiceNameField(c)
					s.NewServiceURLField(c)
					s.NewServiceModelField(c)
				})
				s.NewServiceRow(c)
				s.CardStatusLine(c, TranslateCard{ID: NewCustomServiceID})
			})
		})
}

const CustomServiceCardTitle = "自定义"

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
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(6)).Children(func() {
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
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(6)).Children(func() {
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
			// The order is the reverse of adding one: the service is
			// forgotten first, so that nothing asks an address that is about
			// to be gone, and its key is dropped after.
			err = service.RemoveCustomTranslator(ctx, ID)
			if err == nil {
				err = ForgetCredential(ctx, secrets, ID, key, s.Form.HasCredential[ID])
			}
			if err == nil {
				// And its translator goes with it. The cards are drawn from
				// the registry, so a service taken out of the settings but
				// left in the registry is a card that stays on the page with
				// no address and nothing to ask.
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
			// No window: this state exists only while a test builds the
			// view, and there is nothing to hand the answer to.
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
			// The new service needs a translator before it can be asked.
			if err = s.RegisterOwnServices(); err != nil {
				slog.Warn("the new service could not be registered", "error", err)
			}
		}
		// A custom service can point at any OpenAI-compatible gateway. Its model
		// catalogue does not reliably advertise vision support, so test the
		// exact Endpoint with a tiny image after the key and translator exist.
		// Failure does not undo the service: it can still be a useful Text
		// translator, and the stored capability remains false. It is logged
		// rather than dropped, because the card then tells the user the service
		// is Text-only and this line is what explains why.
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
		// Nothing to store: an optional key that is not there means the
		// service needs none, and it is already Configured.
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
			// A panel now has what it was missing: fetch with it.
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
