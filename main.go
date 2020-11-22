package main

import (
	"log"

	webhookCore "github.com/devops-simba/webhook_core"
)

func main() {
	command := webhookCore.ReadCommand(
		"",    // defaultCommand:	  		use default defaultCommand
		nil,   // supportedCommands: 		no extra command
		false, // dontAddDefaultCommands: 	we want it to include default commands
		NewRenameHostInRouteMutatingWebhook(),
	)

	err := command.Execute()
	if err != nil {
		log.Fatalf("FAILED: %v", err)
	}
}
