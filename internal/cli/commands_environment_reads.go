package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

// registerEnvironmentReads is deliberately bound to an injected authenticated
// enrollment reader. The default CLI has none until project state and
// authorization are deployed; listing must never fall back to candidate files.
func registerEnvironmentReads(root *cobra.Command,out io.Writer,reader client.EnvironmentReader,project,requestedFormat *string){
	parent:=&cobra.Command{Use:"environment",Short:"Inspect enrolled environments",Args:cobra.NoArgs}
	root.AddCommand(parent)
	format:=func()(clioutput.Format,error){
		if !root.PersistentFlags().Changed("output"){return clioutput.Table,nil}
		return clioutput.ParseFormat(*requestedFormat)
	}
	service:=func()*client.EnvironmentsService{return client.New(client.Options{EnvironmentReader:reader}).Environments}
	parent.AddCommand(&cobra.Command{
		Use:"list",Args:cobra.NoArgs,Short:"List environments enrolled in the verified project",
		Example:"  aginctus environment list\n  aginctus environment list --incus-project lab --output=json",
		RunE:func(cmd *cobra.Command,_ []string)error{
			f,err:=format();if err!=nil{return err}
			result,err:=service().List(cmd.Context(),client.ListEnvironmentsRequest{Project:*project})
			if err!=nil{return err}
			return renderEnvironmentList(out,f,result)
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:"show ID|NAME",Args:cobra.ExactArgs(1),Short:"Inspect an enrolled environment by verified UUID or name",
		Example:"  aginctus --incus-project lab environment show <environment-uuid>\n  aginctus --incus-project lab environment show dev",
		RunE:func(cmd *cobra.Command,args []string)error{
			f,err:=format();if err!=nil{return err}
			result,err:=service().Resolve(cmd.Context(),client.ResolveEnvironmentRequest{Project:*project,Selector:args[0]})
			if err!=nil{return err}
			return renderEnvironmentDetail(out,f,result)
		},
	})
}
func renderEnvironmentList(out io.Writer,f clioutput.Format,result api.EnvironmentList)error{
	switch f{
	case clioutput.JSON,clioutput.YAML:return clioutput.WriteStructured(out,f,result)
	case clioutput.Name:
		for _,env:=range result.Items{if _,err:=fmt.Fprintln(out,env.ID);err!=nil{return err}}
		return nil
	case clioutput.Table,clioutput.Wide:
		w:=tabwriter.NewWriter(out,0,0,2,' ',0)
		if f==clioutput.Wide {
			if _,err:=fmt.Fprintln(w,"ID\tNAME\tPROJECT\tSERVER ID");err!=nil{return err}
			for _,env:=range result.Items{if _,err:=fmt.Fprintf(w,"%s\t%s\t%s\t%s\n",env.ID,env.Name,env.Binding.Project,env.Binding.ServerID);err!=nil{return err}}
		}else{
			if _,err:=fmt.Fprintln(w,"ID\tNAME");err!=nil{return err}
			for _,env:=range result.Items{if _,err:=fmt.Fprintf(w,"%s\t%s\n",env.ID,env.Name);err!=nil{return err}}
		}
		return w.Flush()
	default:return fmt.Errorf("unsupported environment list format %q",f)
	}
}
func renderEnvironmentDetail(out io.Writer,f clioutput.Format,result api.EnvironmentDetail)error{
	switch f{
	case clioutput.JSON,clioutput.YAML:return clioutput.WriteStructured(out,f,result)
	case clioutput.Name:_,err:=fmt.Fprintln(out,result.ID);return err
	case clioutput.Table,clioutput.Wide:
		w:=tabwriter.NewWriter(out,0,0,2,' ',0)
		if _,err:=fmt.Fprintf(w,"ID\t%s\nNAME\t%s\nPROJECT\t%s\nSERVER ID\t%s\n",result.ID,result.Name,result.Binding.Project,result.Binding.ServerID);err!=nil{return err}
		return w.Flush()
	default:return fmt.Errorf("unsupported environment detail format %q",f)
	}
}
