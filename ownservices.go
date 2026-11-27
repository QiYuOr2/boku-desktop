package main

import (
	"boku/internal/platform/httpc"
	"boku/internal/translate"
	customtranslator "boku/internal/translate/custom"
	"boku/internal/translate/openai"
)

// ownServices 让注册表与用户自建的服务保持一致。
//
// 每个已存服务一个翻译器，以设置文件保存它的 id 注册。这个集合不在任何地方写死，因为它不固定：用户从设置窗口添加服务，它的翻译器必须在下一次翻译询问前存在。因此注册是可重复执行的操作，重复执行也很便宜——已注册的服务会被跳过。
//
// 它作为类型存在而非 services() 里几行代码，是为了让「追上用户新增内容」成为一次调用，在添加服务的地方调用即可；二者保持一致于是成为代码的性质，而不是调用者必须记住的事。
type ownServices struct {
	client   *httpc.Client
	registry *translate.Registry
	section  *translate.Section
}

// newOwnServices 返回在装配所建的注册表上的登记器。
func newOwnServices(client *httpc.Client, registry *translate.Registry, section *translate.Section) *ownServices {
	return &ownServices{client: client, registry: registry, section: section}
}

// registerAll 让注册表与设置一致：设置中的每个服务都有一个翻译器，已不再持有的服务则没有。
//
// 启动时调用一次，设置窗口在用户添加或删除服务后调用。两个方向是同一件事——窗口画的卡片来自注册表，有哪些服务来自设置，二者必须一致。设置已删除而注册表保留的服务，会是一张没了地址、无从询问却仍留在页面上的卡片。
//
// 已有翻译器的服务保持原样而不重复注册：注册表拒绝重复 id，重新注册一个服务不值得作为一个错误。
func (o *ownServices) registerAll() error {
	present := map[string]bool{}
	for _, id := range o.section.Current().CustomServiceIDs() {
		present[id] = true
		if err := o.register(id); err != nil {
			return err
		}
	}
	for _, id := range o.registry.TranslatorIDs() {
		if present[id] || !translate.IsCustomService(id) {
			continue
		}
		o.registry.Remove(id)
	}
	return nil
}

// register 添加一个服务的翻译器，若已存在则跳过。
func (o *ownServices) register(id string) error {
	if o.registry.Has(id) {
		return nil
	}
	// 地址与名称在每次询问翻译器时才从 section 读取，而非此刻捕获：用户可能在应用运行期间修改它们，下一次翻译应使用修改后的值。数据在内存中，读取不涉及文件。
	settings := o.section
	return o.registry.Register(customtranslator.New(o.client, id, func() openai.CustomService {
		service, _ := settings.Current().CustomService(id)
		return openai.CustomService{
			BaseURL:       service.BaseURL,
			DisplayName:   service.DisplayName,
			Model:         service.Model,
			SupportsImage: service.SupportsImage,
		}
	}))
}
