package mg

import (
	"bufio"
	"fmt"
	"github.com/ugorji/go/codec"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
)

var (
	// DefaultCodec is the name of the default codec used for IPC communication
	DefaultCodec = "json"

	// codecHandles is the map of all valid codec handles
	codecHandles = func() map[string]codec.Handle {
		m := map[string]codec.Handle{
			"cbor": &codec.CborHandle{},
			"json": &codec.JsonHandle{
				Indent:         2,
				TermWhitespace: true,
			},
			"msgpack": &codec.MsgpackHandle{},
		}
		m[""] = m[DefaultCodec]
		return m
	}()

	// CodecNames is the list of names of all valid codec handles
	CodecNames = func() []string {
		l := make([]string, 0, len(codecHandles))
		for k, _ := range codecHandles {
			if k != "" {
				l = append(l, k)
			}
		}
		sort.Strings(l)
		return l
	}()

	// CodecNamesStr is the list of names of all valid codec handles in the form `a, b or c`
	CodecNamesStr = func() string {
		i := len(CodecNames) - 1
		return strings.Join(CodecNames[:i], ", ") + " or " + CodecNames[i]

	}()
)

type AgentConfig struct {
	// Codec is the name of the codec to use for IPC
	// Valid values are json, cbor or msgpack
	// Default: json
	Codec string
}

type AgentReq struct {
	Cookie string
	Action struct {
		Name string
		Data codec.Raw
	}
	Props clientProps
}

type AgentRes struct {
	Cookie string
	Error  string
	State  struct {
		State
		Config interface{}
	}
}

type Agent struct {
	*log.Logger
	Store *Store

	mu sync.Mutex

	stdin  io.ReadCloser
	stdout io.WriteCloser
	stderr io.WriteCloser

	handle codec.Handle
	enc    *codec.Encoder
	dec    *codec.Decoder
}

func (ag *Agent) Run() error {
	defer ag.stdin.Close()
	defer ag.stdout.Close()
	return ag.communicate()
}

func (ag *Agent) communicate() error {
	ag.Println("ready")
	ag.Store.Dispatch(Started{})

	for {
		rq := AgentReq{}
		if err := ag.dec.Decode(&rq); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("ipc.decode: %s", err)
		}

		// TODO: put this on a channel in the future.
		// at the moment we lock the store and block new requests to maintain request/response order
		// but decoding time could become a problem if we start sending large requests from the client
		// we currently only have 1 client (GoSublime) that we also control so it's ok for now...
		ag.Store.syncRq(ag, rq)
	}
	return nil
}

func (ag *Agent) createAction(name string) Action {
	if f := actionCreators[name]; f != nil {
		return f()
	}
	return nil
}

func (ag *Agent) listener(st State) {
	res := AgentRes{}
	res.State.State = st
	ag.send(res)
}

func (ag *Agent) send(res AgentRes) error {
	ag.mu.Lock()
	defer ag.mu.Unlock()

	if res.Error == "" {
		res.Error = strings.Join([]string(res.State.Errors), "\n")
	}

	if res.State.View.changed == 0 {
		res.State.View = View{}
	}

	if ec := res.State.State.Config; ec != nil {
		res.State.Config = ec.EditorConfig()
	}

	return ag.enc.Encode(res)
}

func NewAgent(cfg AgentConfig) (*Agent, error) {
	ag := &Agent{
		Logger: Log,
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		handle: codecHandles[cfg.Codec],
	}
	ag.Store = newStore(ag.listener).Use(DefaultReducers...)
	if e := os.Getenv("MARGO_SUBLIME_INSTALL_FAILED"); e != "" {
		ag.Store.Use(func(st State, _ Action) State { return st.AddStatus(e) })
	}

	if ag.handle == nil {
		return ag, fmt.Errorf("Invalid codec '%s'. Expected %s", cfg.Codec, CodecNamesStr)
	}
	ag.enc = codec.NewEncoder(bufio.NewWriter(ag.stdout), ag.handle)
	ag.dec = codec.NewDecoder(bufio.NewReader(ag.stdin), ag.handle)

	return ag, nil
}

func (ag *Agent) Args() Args {
	return Args{
		Store: ag.Store,
	}
}
