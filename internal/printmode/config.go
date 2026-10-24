package printmode

import (
	"flag"
	"io"
	"log"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/convstore"
)

// Version is the CLI version reported by printmode.
var Version = cli.ReferenceVersion

// ConversationSelection describes a continued or resumed conversation for a turn.
type ConversationSelection struct {
	ID             string
	Source         string
	Store          *convstore.Conversation
	History        []convstore.ChatTurn
	PriorUserTurns int
}

// ConversationResolver resolves the conversation selection from flags.
var ConversationResolver = func(fs *flag.FlagSet, warn io.Writer) ConversationSelection {
	return ConversationSelection{}
}

// BackendPicker resolves the backend.Client for a turn.
var BackendPicker = func(fs *flag.FlagSet, modelName string, w io.Writer) backend.Client {
	return backend.NewMock()
}

// BackendResolver resolves the backend string name from flags.
var BackendResolver = func(fs *flag.FlagSet, w io.Writer) (string, bool) {
	return "mock", false
}

// ModelResolver validates and resolves the model and reasoning effort.
var ModelResolver = func(model, effort string) (string, error) {
	return model, nil
}

var printModeSilenceLogger = log.New(io.Discard, "", 0)
