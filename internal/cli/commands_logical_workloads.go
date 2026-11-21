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

// registerLogicalWorkloadReads wires only an injected, authenticated domain
// reader. Ordinary CLI invocations have no such authority until an approved
// project-scoped state backend is deployed; they fail closed.
func registerLogicalWorkloadReads(root *cobra.Command, stdout io.Writer, reader client.LogicalWorkloadReader, project, environmentID, requestedFormat *string) {
	for _,family := range []struct{name string; class api.WorkloadClass}{
		{"workloads",""},{"agents",api.WorkloadClassAgent},{"frontends",api.WorkloadClassFrontend},{"gateways",api.WorkloadClassGateway},
	} {
	parent := &cobra.Command{Use:family.name,Short:"Read environment-owned logical workloads",Args:cobra.NoArgs}
	root.AddCommand(parent)
	format := func()(clioutput.Format,error){
		if !root.PersistentFlags().Changed("output"){return clioutput.Table,nil}
		return clioutput.ParseFormat(*requestedFormat)
	}
	newService:=func()*client.WorkloadsService{
		return client.New(client.Options{LogicalWorkloadReader:reader}).Workloads
	}
	parent.AddCommand(&cobra.Command{
		Use:"list",Args:cobra.NoArgs,Short:"List verified logical workloads",
		RunE:func(cmd *cobra.Command,_ []string)error{
			f,err:=format();if err!=nil{return err}
			// Same scope and logical result for every format.
			list,err:=newService().List(cmd.Context(),api.ListWorkloadsRequest{Project:*project,EnvironmentID:*environmentID,Class:family.class})
			if err!=nil{return err}
			return renderLogicalWorkloadList(stdout,f,list)
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:"show NAME",Args:cobra.ExactArgs(1),Short:"Inspect an environment-owned workload",
		RunE:func(cmd *cobra.Command,args []string)error{
			f,err:=format();if err!=nil{return err}
			item,err:=newService().Get(cmd.Context(),api.GetWorkloadRequest{Project:*project,EnvironmentID:*environmentID,Name:args[0]})
			if err!=nil{return err}
			if family.class!=""&&item.Class!=family.class{return fmt.Errorf("workload class does not match %s",family.name)}
			return renderLogicalWorkloadDetail(stdout,f,item)
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:"status NAME",Args:cobra.ExactArgs(1),Short:"Inspect workload observations",
		RunE:func(cmd *cobra.Command,args []string)error{
			f,err:=format();if err!=nil{return err}
			if family.class!="" {
				owned,err:=newService().Get(cmd.Context(),api.GetWorkloadRequest{Project:*project,EnvironmentID:*environmentID,Name:args[0]})
				if err!=nil{return err}
				if owned.Class!=family.class{return fmt.Errorf("workload class does not match %s",family.name)}
			}
			item,err:=newService().Status(cmd.Context(),api.WorkloadStatusRequest{Project:*project,EnvironmentID:*environmentID,Name:args[0]})
			if err!=nil{return err}
			return renderLogicalWorkloadStatus(stdout,f,item)
		},
	})
	}
}
func renderLogicalWorkloadList(out io.Writer,f clioutput.Format,list api.WorkloadList)error{
	switch f{
	case clioutput.JSON,clioutput.YAML:return clioutput.WriteStructured(out,f,list)
	case clioutput.Name:
		for _,w:=range list.Items{if _,err:=fmt.Fprintln(out,w.Name);err!=nil{return err}}
		return nil
	case clioutput.Table,clioutput.Wide:
		wr:=tabwriter.NewWriter(out,0,0,2,' ',0)
		if f==clioutput.Wide{
			if _,err:=fmt.Fprintln(wr,"NAME\tCLASS\tIMPLEMENTATION\tCOMPONENTS");err!=nil{return err}
			for _,w:=range list.Items{if _,err:=fmt.Fprintf(wr,"%s\t%s\t%s\t%d\n",w.Name,w.Class,w.Implementation,len(w.Components));err!=nil{return err}}
		}else{
			if _,err:=fmt.Fprintln(wr,"NAME\tCLASS");err!=nil{return err}
			for _,w:=range list.Items{if _,err:=fmt.Fprintf(wr,"%s\t%s\n",w.Name,w.Class);err!=nil{return err}}
		}
		return wr.Flush()
	default:return fmt.Errorf("unsupported output format %q",f)
	}
}
func renderLogicalWorkloadDetail(out io.Writer,f clioutput.Format,item api.WorkloadDetail)error{
	switch f{
	case clioutput.JSON,clioutput.YAML:return clioutput.WriteStructured(out,f,item)
	case clioutput.Name:_,err:=fmt.Fprintln(out,item.Name);return err
	case clioutput.Table,clioutput.Wide:
		wr:=tabwriter.NewWriter(out,0,0,2,' ',0)
		if _,err:=fmt.Fprintf(wr,"NAME\t%s\nCLASS\t%s\nIMPLEMENTATION\t%s\nENVIRONMENT\t%s\n",item.Name,item.Class,item.Implementation,item.EnvironmentID);err!=nil{return err}
		if f==clioutput.Wide{for _,c:=range item.Components{if _,err:=fmt.Fprintf(wr,"COMPONENT\t%s\n",c.Name);err!=nil{return err}}}
		return wr.Flush()
	default:return fmt.Errorf("unsupported output format %q",f)
	}
}
func renderLogicalWorkloadStatus(out io.Writer,f clioutput.Format,item api.WorkloadStatus)error{
	switch f{
	case clioutput.JSON,clioutput.YAML:return clioutput.WriteStructured(out,f,item)
	case clioutput.Name:_,err:=fmt.Fprintln(out,item.Name);return err
	case clioutput.Table,clioutput.Wide:
		wr:=tabwriter.NewWriter(out,0,0,2,' ',0)
		if _,err:=fmt.Fprintln(wr,"COMPONENT\tSTATE");err!=nil{return err}
		for _,c:=range item.Observed{if _,err:=fmt.Fprintf(wr,"%s\t%s\n",c.Name,c.State);err!=nil{return err}}
		return wr.Flush()
	default:return fmt.Errorf("unsupported output format %q",f)
	}
}
