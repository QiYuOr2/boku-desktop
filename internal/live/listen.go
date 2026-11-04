package live

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// audioSource is what loopback.Stream already is.
type audioSource interface {
	PCM() <-chan []byte
	Close() error
}

// translator is the local llama-server. Ready is one probe, not a wait.
type translator interface {
	Ready(ctx context.Context) error
	Translate(ctx context.Context, prompt string) (string, error)
}

// lineState is one change from the pipeline. Unset fields are left as they were.
type lineState struct {
	setDraft       bool
	draft          string
	commit         bool
	commitID       uint64
	commitSource   string
	setTranslation bool
	translationID  uint64
	translation    string
	setSlow        bool
	slow           bool
	setReady       bool
	ready          bool
}

// runListen starts both processes, drops audio until the recognizer has gone
// quiet and the translator answers /health, and translates only a non-empty
// final, one completed sentence at a time. Each process is restarted at most once.
func runListen(ctx context.Context, asrSpec, mtSpec processSpec, audio audioSource, tr translator, entries func() []Entry, publish func(lineState)) error {
	if publish == nil {
		publish = func(lineState) {}
	}
	if entries == nil {
		entries = func() []Entry { return nil }
	}
	parent := ctx
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	ctx = withSeq(ctx)

	var hot atomic.Pointer[running]
	var mtReady atomic.Bool
	asr := &procSlot{spec: asrSpec}
	mt := &procSlot{spec: mtSpec}
	errc := make(chan error, 2)
	var runningProcs sync.WaitGroup
	runningProcs.Add(2)
	go func() {
		defer runningProcs.Done()
		errc <- asr.run(ctx, func(line string) {
			handleLine(ctx, line, tr, entries, publish)
		}, func(proc *running) { hot.Store(proc) })
	}()
	go func() {
		defer runningProcs.Done()
		errc <- mt.run(ctx, nil, nil)
	}()
	go pollTranslator(ctx, mt, tr, &hot, &mtReady, publish)
	go writeAudio(ctx, audio, &hot, &mtReady)

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errc:
		cancel()
	}
	runningProcs.Wait()
	if parent.Err() != nil {
		return nil
	}
	return runErr
}

func pollTranslator(ctx context.Context, mt *procSlot, tr translator, hot *atomic.Pointer[running], ready *atomic.Bool, publish func(lineState)) {
	announced := false
	for ctx.Err() == nil {
		mtUp := false
		if mt.alive() && tr != nil {
			probe, cancel := context.WithTimeout(ctx, time.Second)
			err := tr.Ready(probe)
			cancel()
			mtUp = err == nil
		}
		ready.Store(mtUp)
		asrUp := false
		if proc := hot.Load(); proc != nil {
			select {
			case <-proc.exited:
				hot.CompareAndSwap(proc, nil)
			default:
				asrUp = true
			}
		}
		up := mtUp && asrUp
		if up != announced {
			announced = up
			publish(lineState{setReady: true, ready: up})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func writeAudio(ctx context.Context, audio audioSource, hot *atomic.Pointer[running], mtReady *atomic.Bool) {
	if audio == nil {
		return
	}
	pcm := audio.PCM()
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-pcm:
			if !ok {
				return
			}
			proc := hot.Load()
			if proc == nil || !mtReady.Load() || len(chunk) == 0 {
				continue
			}
			select {
			case <-proc.exited:
				hot.CompareAndSwap(proc, nil)
				continue
			default:
			}
			_ = proc.Write(chunk)
		}
	}
}

type translateSeq struct {
	mu   sync.Mutex
	next uint64
}

type seqKey struct{}

func withSeq(ctx context.Context) context.Context {
	return context.WithValue(ctx, seqKey{}, &translateSeq{})
}

func lineSeq(ctx context.Context) *translateSeq {
	seq, _ := ctx.Value(seqKey{}).(*translateSeq)
	return seq
}

func handleLine(ctx context.Context, line string, tr translator, entries func() []Entry, publish func(lineState)) {
	kind, text, ok := ParseLine(line)
	if !ok || kind == eventSilence {
		return
	}
	if kind == eventPartial {
		publish(lineState{setDraft: true, draft: text})
		return
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	seq := lineSeq(ctx)
	for _, sentence := range splitSentences(text) {
		var id uint64
		if seq != nil && tr != nil {
			seq.mu.Lock()
			seq.next++
			id = seq.next
			seq.mu.Unlock()
		}
		publish(lineState{commit: true, commitID: id, commitSource: sentence, setDraft: true, draft: "", setSlow: true, slow: false})
		if seq != nil && tr != nil {
			go translateFinal(ctx, seq, id, sentence, tr, entries, publish)
		}
	}
}

func translateFinal(ctx context.Context, seq *translateSeq, id uint64, text string, tr translator, entries func() []Entry, publish func(lineState)) {
	timer := time.AfterFunc(2*time.Second, func() {
		seq.mu.Lock()
		latest := seq.next == id
		seq.mu.Unlock()
		if latest {
			publish(lineState{setSlow: true, slow: true})
		}
	})
	defer timer.Stop()
	out, err := tr.Translate(ctx, Prompt(text, Match(text, entries())))
	seq.mu.Lock()
	latest := seq.next == id
	seq.mu.Unlock()
	if err != nil {
		if latest {
			publish(lineState{setSlow: true, slow: true})
		}
		return
	}
	publish(lineState{setTranslation: true, translationID: id, translation: out, setSlow: latest, slow: false})
}
