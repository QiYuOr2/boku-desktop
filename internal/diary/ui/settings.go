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
			presentation.SectionHead(c, head, s.mark, presentation.ColorTextMuted, "日记", "")

			if s.svc == nil {
				presentation.SettingsPanel(c, func() {
					presentation.SettingsRow(c, func() {
						presentation.Notice(c, presentation.NoteWarning, "日记数据库不可用，无法更改设置。")
					})
				})
				return
			}

			// 1. 新建日记默认上下文（地点 / 天气）。
			presentation.SettingsGroup(c, "新建日记默认附加", func() {
				presentation.SettingsRow(c, func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.GroupGap).Children(func() {
						ui.Text(c, "默认地点").FontSize(presentation.SizeBody).FontWeight(500).TextColor(presentation.ColorText).SingleLine().Grow(1)
						ui.Switch(c, &s.sync.draftSettings.LocationEnabled).Label("默认地点").Shrink(0)
					})
					if s.sync.draftSettings.LocationEnabled {
						presentation.InputFrame(c, ui.TextInput(c, &s.sync.draftSettings.LocationPlace).
							Placeholder("城市或地点名称（仅保存地名）").SingleLine())
					}
				})

				presentation.SettingsDivider(c)

				presentation.SettingsRow(c, func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.GroupGap).Children(func() {
						ui.Text(c, "自动获取天气").FontSize(presentation.SizeBody).FontWeight(500).TextColor(presentation.ColorText).SingleLine().Grow(1)
						ui.Switch(c, &s.sync.draftSettings.WeatherEnabled).Label("自动获取天气").Shrink(0)
					})
					if s.sync.draftSettings.WeatherEnabled {
						presentation.InputFrame(c, ui.TextInput(c, &s.sync.draftSettings.WeatherPlace).
							Placeholder("天气城市名称").SingleLine())
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(float32(4)).Children(func() {
							ui.Text(c, "天气数据来源：").FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
							ui.Link(c, diary.WeatherAttribution, diary.WeatherAttributionURL).
								FontSize(presentation.SizeSmall).TextColor(presentation.ColorBrand)
						})
					}
				})
			})

			// 2. 云同步服务端配置（保存在 settings.json，不硬编码进源码）。
			presentation.SettingsGroup(c, "云同步服务", func() {
				presentation.SettingsRow(c, func() {
					ui.Text(c, "同步服务地址").FontSize(presentation.SizeBody).FontWeight(500).TextColor(presentation.ColorText).SingleLine()
					presentation.InputFrame(c, ui.TextInput(c, &s.sync.draftSettings.SyncEndpoint).
						Placeholder("https://sync.example.com").SingleLine()).Font("monospace")
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.RowGap).Children(func() {
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
			})

			// 3. 云同步账号、设备与冲突。
			s.syncAccountSection(c)
		})
	})
}

func (s *State) syncAccountSection(c *ui.Context) {
	st := s.sync.status
	presentation.SettingsGroup(c, "云同步账号", func() {
		if !st.Configured {
			presentation.SettingsRow(c, func() {
				presentation.Notice(c, presentation.NotePlain, "保存上方的同步服务地址后即可登录同步。")
			})
			return
		}

		if !st.LoggedIn {
			presentation.SettingsRow(c, func() {
				s.syncLoginForm(c)
			})
		} else {
			s.syncLoggedInPanel(c, st)
		}

		if s.sync.message != "" {
			presentation.SettingsDivider(c)
			presentation.SettingsRowCompact(c, func() {
				kind := presentation.NotePlain
				if s.sync.messageWarn {
					kind = presentation.NoteWarning
				}
				presentation.Notice(c, kind, s.sync.message)
			})
		}
	})

	if st.Configured && st.LoggedIn {
		s.syncDevicesPanel(c, st.OwnDeviceID)
	}
	if st.Configured && len(s.sync.conflicts) > 0 {
		s.syncConflictsPanel(c)
	}
}

func (s *State) syncLoginForm(c *ui.Context) {
	presentation.Stacked(c, presentation.RowGap, func() {
		if !s.sync.awaitingCode {
			c.Key("sync-login-email")
			presentation.InputFrame(c, ui.TextInput(c, &s.sync.emailInput).
				Placeholder("邮箱地址").SingleLine())
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
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
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
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
	first := true
	row := func(label, value string, mono bool) {
		if !first {
			presentation.SettingsDivider(c)
		}
		first = false
		presentation.SettingsKVRow(c, label, value, mono)
	}
	if st.AccountEmail != "" {
		row("账号", st.AccountEmail, false)
	}
	lastSync := "尚未同步"
	if st.LastSyncedAt != nil {
		lastSync = st.LastSyncedAt.Local().Format("2006-01-02 15:04")
	}
	row("上次同步", lastSync, true)
	row("待发送变更", fmt.Sprintf("%d 条", st.PendingCount), false)
	if st.ConflictCount > 0 {
		row("待处理冲突", fmt.Sprintf("%d 条", st.ConflictCount), false)
	}
	presentation.SettingsDivider(c)
	presentation.SettingsRowCompact(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
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
	presentation.SettingsGroup(c, "已登录设备", func() {
		for index, dev := range s.sync.devices {
			if index > 0 {
				presentation.SettingsDivider(c)
			}
			c.Key(dev.DeviceID)
			d := dev
			isOwn := d.DeviceID == ownDeviceID
			tl, tr, br, bl := presentation.PanelRowRadius(index == 0, index == len(s.sync.devices)-1)
			row := ui.Row(c).FillWidth().
				Padding(presentation.SettingRowPadYCompact, presentation.SettingRowPadX).
				Radius(tl, tr, br, bl).
				AlignItems(ui.Center).
				Gap(presentation.RowGap)
			hot := row.Hovered() || s.sync.confirmRevokeID == d.DeviceID
			if hot {
				row.Background(presentation.ColorHover)
			}
			row.Children(func() {
				ui.Column(c).Grow(1).Gap(float32(4)).Children(func() {
					name := d.DeviceName
					if isOwn {
						name += "（本机）"
					}
					ui.Text(c, name).FontSize(presentation.SizeCardName).FontWeight(500).TextColor(presentation.ColorText).SingleLine()
					sub := platformLabel(d.Platform)
					if d.RevokedAt != nil {
						sub += " · 已撤销"
					} else if d.LastSeenAt != nil {
						sub += " · 最近同步 " + d.LastSeenAt.Local().Format("01-02 15:04")
					}
					ui.Text(c, sub).FontSize(presentation.SizeSmall).TextColor(presentation.ColorTextMuted).SingleLine()
				})
				if d.RevokedAt == nil {
					label := "撤销"
					if s.sync.confirmRevokeID == d.DeviceID {
						label = "确认撤销"
					}
					actions := ui.Row(c).Shrink(0)
					if !hot {
						actions = actions.Opacity(0).PassThrough()
					}
					actions.Children(func() {
						if presentation.TextAction(c, label, presentation.ColorDanger).Clicked() && !s.sync.busy {
							s.RequestRevokeDevice(d.DeviceID)
						}
					})
				}
			})
		}
	})
}

func (s *State) syncConflictsPanel(c *ui.Context) {
	presentation.SettingsGroup(c, "待处理冲突", func() {
		for index, item := range s.sync.conflicts {
			if index > 0 {
				presentation.SettingsDivider(c)
			}
			c.Key(item.EntityID)
			it := item
			presentation.SettingsRow(c, func() {
				titleLine := it.LocalTitle
				if it.DiaryDate != "" {
					if titleLine != "" {
						titleLine = it.DiaryDate + " · " + titleLine
					} else {
						titleLine = it.DiaryDate
					}
				}
				ui.Text(c, titleLine).FontSize(presentation.SizeCardName).FontWeight(600).TextColor(presentation.ColorWarning).SingleLine()
				ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(presentation.FieldGap).Children(func() {
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
