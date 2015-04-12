package proto

func (e *Entry) RebuildCommand(profile *Profile, graph *Graph) {
	author := e.From.Id
	commands := []string{"comment"}
	if _, ok := graph.Admins[author]; ok {
		commands = append(commands, "edit", "delete")
	}
	if _, ok := graph.Subscriptions[author]; ok {
		// private check
	}
	if profile.Id == e.From.Id {
		commands = append(commands, "edit", "delete")
	} else {
		commands = append(commands, "like")
	}
	e.Commands = commands
	return
}
