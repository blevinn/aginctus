package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

type configuredProjectPreference struct{ cfg *config.Config }

func (s configuredProjectPreference) SelectedIncusProject(ctx context.Context) (string,error) {
	if err:=ctx.Err();err!=nil{return "",err}
	if s.cfg==nil {return "",fmt.Errorf("configuration unavailable")}
	scope,err:=s.cfg.IncusProjectScope()
	if err!=nil{return "",err}
	return scope.Name,nil
}

// addConfigScope displays only a deliberately nonsecret configured selector.
// It does not authenticate the project or enumerate a state service.
func addConfigScope(parent *cobra.Command,root *cobra.Command,out io.Writer,
	load func()(*config.Config,error),requestedFormat *string) {
	parent.AddCommand(&cobra.Command{
		Use:"scope",Short:"Show selected Incus project preference (not authority)",Args:cobra.NoArgs,
		RunE:func(cmd *cobra.Command,_ []string)error{
			cfg,err:=load();if err!=nil{return nil}
			result,err:=client.New(client.Options{
				ConfigSelectionReader:configuredProjectPreference{cfg:cfg},
			}).Config.Selection(cmd.Context())
			if err!=nil{return err}
			format:=clioutput.Table
			if root.PersistentFlags().Changed("output"){
				format,err=clioutput.ParseFormat(*requestedFormat)
				if err!=nil{return err}
			}
			return renderConfigScope(out,format,result)
		},
	})
}
func renderConfigScope(out io.Writer,f clioutput.Format,result api.ConfigSelection)error{
	switch f{
	case clioutput.JSON,clioutput.YAML:return clioutput.WriteStructured(out,f,result)
	case clioutput.Name:_,err:=fmt.Fprintln(out,result.IncusProject);return err
	case clioutput.Table,clioutput.Wide:
		w:=tabwriter.NewWriter(out,0,0,2,' ',0)
		if _,err:=fmt.Fprintf(w,"INCUS PROJECT\t%s\nAUTHORITY\t%s\n",result.IncusProject,result.Authority);err!=nil{return err}
		return w.Flush()
	default:return fmt.Errorf("unsupported config selection output format %q",f)
	}
}
