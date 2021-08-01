package pb

import "fmt"

func (e *Entry) RebuildCommand(profile *Profile, graph *Graph) {
	if profile.Id == "" {
		e.Commands = []string{}
		return
	}

	ownerOrSuper := false
	commands := []string{"comment"}
	if profile.IsSuper {
		ownerOrSuper = true
	}
	if _, ok := graph.Admins[profile.Id]; ok {
		if !ownerOrSuper {
			ownerOrSuper = true
		}
	}
	// FIXME: subscriptions may huge
	// if _, ok := graph.Subscriptions[author]; ok {
	// 	// private check
	// }
	if profile.Id == e.From.Id {
		if !ownerOrSuper {
			ownerOrSuper = true
		}
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

	// place it in the end
	if ownerOrSuper {
		commands = append(commands, "edit", "delete")
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
			Body:        fmt.Sprintf("%d other people", length-3),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		e.Likes = e.Likes[:3]
		e.Likes = append(e.Likes, collapsing)
	}
}
