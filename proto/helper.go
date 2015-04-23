package proto

import "fmt"

func (e *Entry) RebuildCommand(profile *Profile, graph *Graph) {
	if profile.Id == "" {
		e.Commands = []string{}
		return
	}

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
		// liked?
		liked := false
		for _, like := range e.Likes {
			// TODO: fixme, why on earth like.From == nil?
			if like.From != nil && like.From.Id == profile.Id {
				liked = true
				break
			}
		}
		if liked {
			commands = append(commands, "unlike")
		} else {
			commands = append(commands, "like")
		}
	}
	e.Commands = commands
	return
}

func (e *Entry) RebuildCommentsCommand(profile *Profile, graph *Graph) {
	for _, cmt := range e.Comments {
		cmt.Commands = []string{}
		if cmt.From == nil || profile.Id == "" {
			continue
		}
		if profile.Id == cmt.From.Id {
			cmt.Commands = []string{"edit", "delete"}
		}
	}
}

func (e *Entry) FormatComments(max int32) {
	// collapse comments
	length := len(e.Comments)
	if max == 0 && length > 4 {
		collapsing := &Comment{
			Body:        fmt.Sprintf("%d more comments", length-2),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		e.Comments = []*Comment{e.Comments[0], collapsing, e.Comments[length-1]}
	}
}

func (e *Entry) FormatLikes(max int32) {
	// collapse likes
	length := len(e.Likes)
	if max == 0 && length > 4 {
		collapsing := &Like{
			Body:        fmt.Sprintf("%d other people", length-2),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		e.Likes = e.Likes[:3]
		e.Likes = append(e.Likes, collapsing)
	}
}
