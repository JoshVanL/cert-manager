/*
Copyright 2019 The Jetstack cert-manager contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"github.com/spf13/cobra"
)

var requestCertCmd = &cobra.Command{
	Use:     "certicate",
	Short:   "Request a signed certificate from cert-manager.",
	Aliases: []string{"cert"},
	RunE: func(cmd *cobra.Command, args []string) error {
		//client, err := client.New(flags.Kubeconfig)
		//if err != nil {
		//	return err
		//}

		//request := request.New(client, &flags.Request)
		//mustDie(request.Cert())

		return nil
	},
}

func init() {
	requestCertFlags(requestCertCmd.PersistentFlags())
	requestCmd.AddCommand(requestCertCmd)
}
