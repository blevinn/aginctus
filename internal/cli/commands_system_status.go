package cli

import(
	"fmt"
	"io"
	"text/tabwriter"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

// registerSystemStatus only exposes observed server metadata. It never
// authenticates environment ownership or asserts that workloads are healthy.
func registerSystemStatus(root *cobra.Command,out io.Writer,server IncusClient,requestedFormat *string){
	system:=&cobra.Command{Use:"system",Short:"Inspect local Incus system observations",Args:cobra.NoArgs}
	root.AddCommand(system)
	system.AddCommand(&cobra.Command{
		Use:"status",Short:"Read connected Incus server version",Args:cobra.NoArgs,
		RunE:func(cmd *cobra.Command,_ []string)error{
			result,err:=client.New(client.Options{SystemReader:server}).System.Status(cmd.Context())
			if err!=nil{return err}
			format:=clioutput.Table
			if root.PersistentFlags().Changed("output"){
				format,err=clioutput.ParseFormat(*requestedFormat)
				if err!=nil{return err}
			}
			return renderSystemStatus(out,format,result)
		},
	})
}
func renderSystemStatus(out io.Writer,format clioutput.Format,result api.SystemStatus)error{
	switch format {
	case clioutput.JSON,clioutput.YAML:return clioutput.WriteStructured(out,format,result)
	case clioutput.Name:_,err:=fmt.Fprintln(out,result.IncusVersion);return err
	case clioutput.Table,clioutput.Wide:
		w:=tabwriter.NewWriter(out,0,0,2,' ',0)
		if _,err:=fmt.Fprintf(w,"INCUS VERSION\t%s\nSCOPE\t%s\n",result.IncusVersion,result.Scope);err!=nil{return err}
		return w.Flush()
	default:return fmt.Errorf("unsupported system status format %q",format)
	}
}
