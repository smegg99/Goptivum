// cmd/status.go

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/ui"
	"github.com/smegg99/goptivum/backend/version"
)

// probeTimeout bounds one request to the server being asked about.
const probeTimeout = 3 * time.Second

// maxProbeBody bounds what a reply may be, because the thing answering may not
// be a Goptivum Server at all.
const maxProbeBody = 64 << 10

func newStatusCmd(s *server) *cobra.Command {
	return &cobra.Command{
		Use:         "status",
		Annotations: map[string]string{annotationConfig: "yes"},
		Args:        s.noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return s.status(cmd.Context())
		},
	}
}

func (s *server) status(ctx context.Context) error {
	base := "http://" + s.settings.Server.Address
	out, say := s.print(), s.say()
	out.Line(out.S.Title, "goptivum-server")
	out.Blank()

	info, err := readInfo(ctx, base)
	if err != nil {
		// Nothing answered, so the address is wrong or nothing is listening.
		// That is a different problem from a server that answers badly, and
		// the two have different fixes.
		out.Fields(
			ui.Field{Label: messages.CliStatusAddress(say), Value: base},
			ui.Field{Label: messages.CliStatusReached(say), Value: out.Problem(messages.CliStatusNo(say))},
		)
		out.Blank()
		out.Panel(out.S.Muted.Render(err.Error()))
		return errors.New(messages.CliStatusDidNotAnswer(say,
			messages.CliStatusDidNotAnswerParams{Address: base}))
	}

	product := out.State(true, string(info.Product))
	if info.Product != gen.Goptivum {
		product = out.Problem(messages.CliStatusNotGoptivum(say,
			messages.CliStatusNotGoptivumParams{Product: string(info.Product)}))
	}
	speaks := out.State(true, info.ApiVersion)
	if info.ApiVersion != version.API {
		speaks = out.Warning(messages.CliStatusOtherApiVersion(say,
			messages.CliStatusOtherApiVersionParams{Served: info.ApiVersion, Known: version.API}))
	}

	ready, readyErr := readReady(ctx, base, info.ApiVersion)
	readyState := out.State(true, messages.CliStatusReady(say))
	switch {
	case readyErr != nil:
		readyState = out.Problem(messages.CliStatusUnreachable(say))
	case !ready:
		readyState = out.Warning(messages.CliStatusDependencyDown(say))
	}

	out.Fields(
		ui.Field{Label: messages.CliStatusAddress(say), Value: base},
		ui.Field{Label: messages.CliStatusProduct(say), Value: product},
		ui.Field{Label: messages.CliStatusApiVersion(say), Value: speaks},
		ui.Field{Label: messages.CliStatusServerVersion(say), Value: info.ServerVersion},
		ui.Field{Label: messages.CliStatusMinClient(say), Value: info.MinClientVersion},
		ui.Field{Label: messages.CliStatusReadiness(say), Value: readyState},
	)
	if info.Product != gen.Goptivum {
		return errors.New(messages.CliStatusNotAServer(say,
			messages.CliStatusNotAServerParams{Address: base}))
	}
	return nil
}

// readInfo reads the frozen discovery endpoint. Its path carries no version,
// because a client has to learn the version before it can call a versioned
// route.
func readInfo(ctx context.Context, base string) (gen.Info, error) {
	var info gen.Info
	body, err := get(ctx, base+"/api/info")
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return info, fmt.Errorf("%s answered something that is not server information", base+"/api/info")
	}
	return info, nil
}

// readReady asks the version the server said it serves, not the one this build
// was compiled against.
func readReady(ctx context.Context, base, apiVersion string) (bool, error) {
	if apiVersion == "" {
		apiVersion = version.API
	}
	response, err := send(ctx, base+"/api/"+apiVersion+"/ready")
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK, nil
}

func get(ctx context.Context, url string) ([]byte, error) {
	response, err := send(ctx, url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", url, response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, maxProbeBody))
}

func send(ctx context.Context, url string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return (&http.Client{Timeout: probeTimeout}).Do(request)
}
