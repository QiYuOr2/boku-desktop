package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"boku/internal/diary"
	"boku/internal/presentation"
)

// SettingsView 绘制设置窗口中的「日记」一节：默认位置/天气、云同步服务配置、账号登录、
// 设备管理与冲突处理。
func (s *State) SettingsView(c *ui.Context, head *ui.Handle) {
	presentation.Inset(c, func() {
		presentation.Stacked(c, presentation.GroupGap, func() {
			presentation.SectionHead(c, head, s.mark, presentation.ColorStoneInk, "日记", "本地记录与云同步")

			if s.svc == nil {
				presentation.Notice(c, presentation.NoteWarning, "日记数据库不可用，无法更改设置。")
				return
			}

			// 1. 新建日记默认上下文（地点 / 天气）。
			presentation.Stacked(c, presentation.RowGap, func() {
				presentation.SectionLabel(c, "新建日记默认附加")

				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, "默认地点").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextSoft).SingleLine().Grow(1)
					ui.Switch(c, &s.sync.draftSettings.LocationEnabled)
				})
				if s.sync.draftSettings.LocationEnabled {
					presentation.InputFrame(c, ui.TextInput(c, &s.sync.draftSettings.LocationPlace).
						Placeholder("城市或地点名称（仅保存地名）").SingleLine())
				}

				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, "自动获取天气").FontSize(presentation.SizeBody).TextColor(presentation.ColorTextSoft).SingleLine().Grow(1)
					ui.Switch(c, &s.sync.draftSettings.WeatherEnabled)
				})
				if s.sync.draftSettings.WeatherEnabled {
					presentation.InputFrame(c, ui.TextInput(c, &s.sync.draftSettings.WeatherPlace).
						Placeholder("天气城市名称").SingleLine())
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Text(c, "天气数据来源：").FontSize(presentation.SizeFine).TextColor(presentation.ColorTextMuted).SingleLine()
						ui.Link(c, diary.WeatherAttribution, diary.WeatherAttributionURL).
							FontSize(presentation.SizeFine).TextColor(presentation.ColorBrand)
					})
				}
			})

			presentation.Hairline(c)

			// 2. 云同步服务端配置（保存在 settings.json，不硬编码进源码）。
			presentation.Stacked(c, presentation.RowGap, func() {
				presentation.SectionLabel(c, "云同步服务")

				presentation.Stacked(c, 6, func() {
					ui.Text(c, "同步服务地址").FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
					presentation.InputFrame(c, ui.TextInput(c, &s.sync.draftSettings.SyncEndpoint).
						Placeholder("https://sync.example.com").SingleLine())
				})

				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					if presentation.DesignButton(c, "保存配置", presentation.ButtonPrimary).Clicked() {
						s.SaveDiarySettings()
					}
					if s.sync.settingsMsg != "" {
						kind := presentation.NoteSuccess
						if s.sync.settingsWarn {
							kind = presentation.NoteWarning
						}
						presentation.Notice(c, kind, s.sync.settingsMsg)
					}
				})
			})

			presentation.Hairline(c)

			// 3. 云同步账号与状态。
			s.syncAccountSection(c)
		})
	})
}

func (s *State) syncAccountSection(c *ui.Context) {
	st := s.sync.status
	presentation.Stacked(c, presentation.RowGap, func() {
		presentation.SectionLabel(c, "云同步账号")

		if !st.Configured {
			presentation.Notice(c, presentation.NotePlain, "保存上方的同步服务地址后即可登录同步。")
			return
		}

		if !st.LoggedIn {
			s.syncLoginForm(c)
		} else {
			s.syncLoggedInPanel(c, st)
		}

		if s.sync.message != "" {
			kind := presentation.NotePlain
			if s.sync.messageWarn {
				kind = presentation.NoteWarning
			}
			presentation.Notice(c, kind, s.sync.message)
		}

		if st.LoggedIn {
			s.syncDevicesPanel(c, st.OwnDeviceID)
		}
		if len(s.sync.conflicts) > 0 {
			s.syncConflictsPanel(c)
		}
	})
}

func (s *State) syncLoginForm(c *ui.Context) {
	presentation.Stacked(c, presentation.RowGap, func() {
		if !s.sync.awaitingCode {
			c.Key("sync-login-email")
			presentation.InputFrame(c, ui.TextInput(c, &s.sync.emailInput).
				Placeholder("邮箱地址").SingleLine())
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
				label := "发送验证码"
				if s.sync.busy {
					label = "发送中…"
				}
				if presentation.DesignButton(c, label, presentation.ButtonPrimary).Clicked() && !s.sync.busy {
					s.SendLoginCode()
				}
			})
			presentation.Notice(c, presentation.NotePlain, "登录后的设备凭据以明文保存于本地 secrets.json。")
			return
		}

		c.Key("sync-login-code")
		ui.Text(c, "验证码已发往 "+strings.TrimSpace(s.sync.emailInput)).
			FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
		presentation.InputFrame(c, ui.TextInput(c, &s.sync.codeInput).
			Placeholder("6 位邮箱验证码").SingleLine())
		presentation.InputFrame(c, ui.TextInput(c, &s.sync.deviceNameInput).
			Placeholder("本机名称").SingleLine())
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
			label := "登录"
			if s.sync.busy {
				label = "登录中…"
			}
			if presentation.DesignButton(c, label, presentation.ButtonPrimary).Clicked() && !s.sync.busy {
				s.CompleteLogin()
			}
			if presentation.DesignButton(c, "重新发送", presentation.ButtonSecondary).Clicked() && !s.sync.busy {
				s.SendLoginCode()
			}
			if presentation.DesignButton(c, "返回", presentation.ButtonSecondary).Clicked() && !s.sync.busy {
				s.CancelLogin()
			}
		})
	})
}

func (s *State) syncLoggedInPanel(c *ui.Context, st diary.SyncStatus) {
	presentation.Stacked(c, 10, func() {
		if st.AccountEmail != "" {
			kvRow(c, "账号", st.AccountEmail)
		}
		lastSync := "尚未同步"
		if st.LastSyncedAt != nil {
			lastSync = st.LastSyncedAt.Local().Format("2006-01-02 15:04")
		}
		kvRow(c, "上次同步", lastSync)
		kvRow(c, "待发送变更", fmt.Sprintf("%d 条", st.PendingCount))
		if st.ConflictCount > 0 {
			kvRow(c, "待处理冲突", fmt.Sprintf("%d 条", st.ConflictCount))
		}

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(4, 0, 0, 0).Children(func() {
			syncLabel := "立即同步"
			if s.sync.syncing {
				syncLabel = "同步中…"
			}
			if presentation.DesignButton(c, syncLabel, presentation.ButtonPrimary).Clicked() && !s.sync.syncing {
				s.SyncNow()
			}

			logoutLabel := "退出登录"
			logoutTone := presentation.ButtonSecondary
			if s.sync.confirmLogout {
				logoutLabel = "确认退出（保留本地日记）"
				logoutTone = presentation.ButtonWarning
			}
			if presentation.DesignButton(c, logoutLabel, logoutTone).Clicked() && !s.sync.busy {
				s.RequestLogout()
			}
			if s.sync.confirmLogout {
				if presentation.TextAction(c, "取消", presentation.ColorTextMuted).Clicked() {
					s.sync.confirmLogout = false
					s.host.Refresh()
				}
			}
		})
	})
}

func (s *State) syncDevicesPanel(c *ui.Context, ownDeviceID string) {
	if len(s.sync.devices) == 0 {
		return
	}
	presentation.Hairline(c)
	presentation.Stacked(c, 8, func() {
		presentation.SectionLabel(c, "已登录设备")
		for _, dev := range s.sync.devices {
			c.Key(dev.DeviceID)
			d := dev
			isOwn := d.DeviceID == ownDeviceID
			ui.Row(c).FillWidth().
				Padding(8, 12, 8, 12).
				Radius(presentation.RadiusField).
				Background(presentation.ColorField).
				Border(1, presentation.ColorBorder).
				AlignItems(ui.Center).
				Gap(10).
				Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						name := d.DeviceName
						if isOwn {
							name += "（本机）"
						}
						ui.Text(c, name).FontSize(presentation.SizeCardName).FontWeight(500).TextColor(presentation.ColorTextSoft).SingleLine()
						sub := platformLabel(d.Platform)
						if d.RevokedAt != nil {
							sub += " · 已撤销"
						} else if d.LastSeenAt != nil {
							sub += " · 最近同步 " + d.LastSeenAt.Local().Format("01-02 15:04")
						}
						ui.Text(c, sub).FontSize(presentation.SizeFine).TextColor(presentation.ColorTextMuted).SingleLine()
					})
					if d.RevokedAt == nil {
						label := "撤销"
						if s.sync.confirmRevokeID == d.DeviceID {
							label = "确认撤销"
						}
						if presentation.TextAction(c, label, presentation.ColorDanger).Clicked() && !s.sync.busy {
							s.RequestRevokeDevice(d.DeviceID)
						}
					}
				})
		}
	})
}

func (s *State) syncConflictsPanel(c *ui.Context) {
	presentation.Hairline(c)
	presentation.Stacked(c, 10, func() {
		presentation.SectionLabel(c, "待处理冲突")
		for _, item := range s.sync.conflicts {
			c.Key(item.EntityID)
			it := item
			ui.Column(c).FillWidth().
				Padding(10, 12, 10, 12).
				Radius(presentation.RadiusField).
				Background(presentation.ColorWarningFill).
				Border(1, presentation.ColorWarningBorder).
				Gap(8).
				Children(func() {
					titleLine := it.LocalTitle
					if it.DiaryDate != "" {
						if titleLine != "" {
							titleLine = it.DiaryDate + " · " + titleLine
						} else {
							titleLine = it.DiaryDate
						}
					}
					ui.Text(c, titleLine).FontSize(presentation.SizeCardName).FontWeight(600).TextColor(presentation.ColorTextSoft).SingleLine()
					ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
						cloudLabel := "保留云端"
						cloudTone := presentation.ButtonSecondary
						if s.sync.confirmKeepCloudID == it.EntityID {
							cloudLabel = "确认丢弃本地修改"
							cloudTone = presentation.ButtonWarning
						}
						if presentation.CardButton(c, cloudLabel, cloudTone).Clicked() && !s.sync.busy {
							s.RequestResolveKeepCloud(it.EntityID)
						}

						localLabel := "本地另存为新日记"
						localTone := presentation.ButtonSecondary
						if s.sync.confirmKeepLocalID == it.EntityID {
							localLabel = "确认另存并同步云端"
							localTone = presentation.ButtonPrimary
						}
						if presentation.CardButton(c, localLabel, localTone).Clicked() && !s.sync.busy {
							s.RequestResolveKeepLocalAsNew(it.EntityID)
						}
					})
				})
		}
	})
}

func kvRow(c *ui.Context, key, value string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, key).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine().Width(90)
		ui.Text(c, value).FontSize(presentation.SizeBody).TextColor(presentation.ColorTextSoft).SingleLine().Grow(1)
	})
}

func platformLabel(p string) string {
	switch p {
	case "ios":
		return "iOS"
	case "macos":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return p
	}
}
