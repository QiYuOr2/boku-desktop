package ui

type testNotifier struct {
	supported bool
	notices   []testNotice
	open      func()
}
type testNotice struct {
	id, title, body string
}

func (n *testNotifier) Supported() bool { return n.supported }

func (n *testNotifier) Notify(id, title, body string, open func()) {
	n.notices = append(n.notices, testNotice{id: id, title: title, body: body})
	n.open = open
}
