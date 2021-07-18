import React from 'react';
import { EntryContent } from './content'
import {getJSON, postJSON, postForm, intersperse} from './utils';


export class Entry extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      entry: this.props.entry,
      onpage_edit: this.props.onpage_edit,
      comments: this.props.entry.comments,
      likes: this.props.entry.likes,
      self_updating: false,
      new_comment_form: false,
      expanded_likes: false,
      expanded_comments: false,
      is_deleted: false,
      comment_preserve: null
    };
  }

  updateState(newState) {
    newState.self_updating = true;
    this.setState(newState);
  }

  // The new static getDerivedStateFromProps lifecycle is invoked after a component
  // is instantiated as well as before it is re-rendered. It can return an object to
  // update state, or null to indicate that the new props do not require any state updates.
  static getDerivedStateFromProps(nextProps, state) {
    // allways return self state, safe?
    if (state.self_updating) {
      console.log("no update top due to self-updating performed");
      // reset self-updating, no merge props here
      state.self_updating = false;
      return state;
    }

    var new_state = Object.assign({}, state);

    if (state.onpage_edit) {
      console.log("no update entry content when on-page editing");
    } else {
      new_state.entry = nextProps.entry
    }

    // compare props from top component
    // merge partial data if state not change
    if (!state.expanded_comments && nextProps.entry.comments) {
      var safe_update = true;
      var comments = state.comments || [];
      comments.forEach(function(cmt) {
        if (cmt.is_editing) {
          safe_update = false;
        }
      });
      if (safe_update) {
        new_state.comments = nextProps.entry.comments;
      }
    }
    if (!state.expanded_likes) {
      new_state.likes = nextProps.entry.likes;
    }
    return new_state;
  }

  handleEdit = (event) => {
    console.log("edit entry")
    // var onpageEdit = !this.state.onpage_edit;
    this.setState({onpage_edit: true});
  }

  handleDelete = () => {
    var entry = this.state.entry;
    postJSON("/a/delete", {entry: entry.id})
      .then(data => {
        var new_state = Object.assign({}, this.state);
        new_state.is_deleted = true;
        this.updateState(new_state);
      });
  }

  handleNewComment = () => {
    if (this.state.new_comment_form) {
      // focus
      React.findDOMNode(this.refs.commentInput).focus();
    } else {
      // make form
      this.updateState({new_comment_form: true});
    }
  }

  submitComment = (id, comment, event) => {
    event.preventDefault();
    var comments = this.state.comments || [];
    var args = {
      entry: this.props.entry.id,
      body: comment
    };
    if (id) {
      args.id = id;
    }
    return postJSON("/a/comment", args)
      .then(comment => { // arrow function
        if (id) {
          var cmts = comments.map(function(cmt, index) {
            if (id === cmt.id) {
              comment.is_editing = false;
              return comment;
            }
            return cmt;
          });
          this.updateState({comments: cmts});
        } else {
          comments.push(comment);
          this.updateState({
            comments: comments,
            new_comment_form: false
          });
        }
      });
  }

  cancelComment = (id, body, event) => {
    if (id) {
      var comments = [];
      this.state.comments.forEach(function(cmt, index) {
        if (id === cmt.id) {
          cmt.is_editing = false;
        }
        comments.push(cmt);
      });
      this.updateState({comments: comments});
    } else {
      if (body) {
        this.updateState({comment_preserve: body});
      }
      this.updateState({new_comment_form: false});
    }
  }

  expandComments = () => {
    getJSON("/a/entry/" + this.props.entry.id)
      .then(data => { // arrow function
        this.setState({
          expanded_comments: true,
          comments: data
        });
      });
  }

  expandLikes = () => {
    getJSON("/a/expandlikes/" + this.props.entry.id)
      .then(data => { // arrow function
        this.updateState({
          expanded_likes: true,
          likes: data
        });
      });
  }

  editComment = (comment) => {
    var comments = [];
    this.state.comments.forEach(function(cmt, index) {
      if (comment.id && comment.id === cmt.id) {
        cmt.is_editing = true;
      }
      comments.push(cmt);
    });
    this.updateState({comments: comments});
  }

  deleteComment = (comment) => {
    if (!comment.id) {
      return comment;
    }
    var data = {entry: this.state.entry.id, comment: comment.id}
    postJSON("/a/comment/delete", data)
      .then(data => {
        comment.body = "comment deleted";
        var comments = [];
        this.state.comments.forEach(function(cmt, index) {
          if (comment.id && comment.id !== cmt.id) {
            comments.push(cmt);
          }
        });

        this.updateState({comments: comments});
      });
    return null;
  }

  handleLike = () => {
    var entry = this.state.entry;
    postJSON("/a/like", {entry: entry.id})
      .then(likes => { // arrow function
        entry.commands.forEach(function(cmd, index) {
          if (cmd === "like") {
            entry.commands[index] = "unlike";
          }
        });
        this.updateState({likes: likes});
      });
  }

  handleUnlike = () => {
    var entry = this.state.entry;
    postJSON("/a/like/delete", {entry: entry.id})
      .then(likes => { // arrow function
        entry.commands.forEach(function(cmd, index) {
          if (cmd === "unlike") {
            entry.commands[index] = "like";
          }
        });
        this.updateState({likes: likes});
      });
  }

  onPostEntry = (formData) => {
    // on post
    return postForm("/a/share", formData)
      .then(entry => {
        var new_state = Object.assign({}, this.state);
        new_state.self_updating = true;
        new_state.onpage_edit = false;
        if (this.state.entry.id !== entry.id) {
          console.log("update failed, new entry created?")
        }
        new_state.entry = entry;
        this.updateState(new_state);
      });
  }

  render() {
    var entry = this.state.entry;
    var edit_mode = this.state.onpage_edit || false;
    var bodyClass = edit_mode? 'editBody' : 'body';

    if (this.state.is_deleted) {
        return (
        <div className="entry" data-eid={entry.id}>
          <div className="body">
              entry deleted.
          </div>
        </div>
        )
    }

    var medias = "";
    if (entry.thumbnails) {
      medias = <EntryMediaBox thumbs={entry.thumbnails} />;
    }

    if (this.state.comments) {
      var self = this;
      var comments = this.state.comments.map(function(comment, index) {
        if (comment.is_editing) {
          return (
            <EntryCommentForm commentId={comment.id}
                              commentBody={comment.rawBody}
                              onSubmitComment={self.submitComment}
                              onCancelComment={self.cancelComment}/>
          );
        } else {
          return (
            <EntryComment comment={comment}
                          expandComments={self.expandComments}
                          editComment={self.editComment}
                          deleteComment={self.deleteComment}
                          key={index} />
          );
        }
      });
    }

    var form_cmt = null;
    if (this.state.new_comment_form) {
      form_cmt = <EntryCommentForm commentBody={this.state.comment_preserve}
                                   onSubmitComment={this.submitComment}
                                   onCancelComment={this.cancelComment}/>

    }

    return (
      <div className="entry" data-eid={entry.id}>
        { edit_mode !== true &&
          <EntryPicture feed={entry.from} />
        }
        <div className={bodyClass}>
          {edit_mode !== true &&
            <EntryAuthor from={entry.from} to={entry.to} />
          }
          <EntryContent
            id={entry.id}
            body={entry.body}
            rawBody={entry.rawBody}
            onpage={edit_mode}
            onPostEntry={this.onPostEntry} />
          {medias}
          <EntryInfo entry={entry}
                     onNewComment={this.handleNewComment}
                     onLike={this.handleLike}
                     onUnlike={this.handleUnlike}
                     onEdit={this.handleEdit}
                     onDelete={this.handleDelete}/>
          <EntryLikes likes={this.state.likes}
                      expandLikes={this.expandLikes} />
          {comments}
          {form_cmt}
        </div>
      </div>
    );
  }
}

function EntryPicture(props) {
  var feed = props.feed;
  return (
    <div className="picture">
      <a href={'/feed/'+feed.id}>
        <img src={feed.picture} alt={feed.title} /></a>
    </div>
  );
}

function EntryToFeeds(props) {
  var feeds = props.feeds.map(function(feed, index) {
    return (
      <EntryToFeed feed={feed} key={feed.id+index} />
    );
  });
  feeds = intersperse(feeds, ", ");

  return (
    <span className="to">{" to "}
      {feeds}
    </span>      
  )
}

function EntryToFeed(props) {
  return <a href={'/feed/' + props.feed.id}>{props.feed.name}</a>;
}

function EntryAuthor(props) {
  var from = props.from;

  var toFeeds;
  if (props.to) {
    toFeeds = <EntryToFeeds feeds={props.to} />;
  } else {
    toFeeds = "";
  }

  return (
    <div className="author">
      <span className="from">
        <EntryToFeed feed={from} />
      </span>
      {toFeeds}
    </div>
  );
}

function EntryMedia(props) {
  var thumb = props.thumb;
  if (thumb.width && thumb.height) {
    var attrs = {
      width: thumb.width+"px",
      height: thumb.height+"px"
    }
    return (
      <a href={thumb.link}>
        <img src={thumb.url} style={attrs} alt="" />
      </a>
    );
  } else {
    return (
      <a href={thumb.link}>
        <img src={thumb.url} alt="" />
      </a>
    );
  }
}

function EntryMediaBox(props) {
  var medias = props.thumbs.map(function(thumb, index) {
    return (
      <EntryMedia thumb={thumb} key={index} />
    );
  });

  return (
    <div className="media">
      {medias}
    </div>
  );
}

function EntryInfo(props) {
  var entry = props.entry;
  var infos = [];
  var via = null;
  if (entry.via) {
    via = <span className="item">
      {" from "}<a href={entry.via.url} className='via'>{entry.via.name}</a>
    </span>;
  }

  if (entry.commands) {
    infos = entry.commands.map(function(cmd, idx) {
      var btn = null
      var liked = false;
      switch (cmd) {
        case "comment":
          btn = <EntryCommandComment onNewComment={props.onNewComment} />;
          break;
        case "like":
          btn = <EntryCommandLike onLike={props.onLike} liked={liked} />;
          break;
        case "unlike":
          liked = true;
          btn = <EntryCommandLike onUnlike={props.onUnlike} liked={liked} />;
          break;
        case "edit":
          btn = <EntryCommandEdit onEdit={props.onEdit} />;
          break;
        case "delete":
          btn = <EntryCommandDelete onDelete={props.onDelete} />;
          break;
        default:
          break;
      }
      return (
        <span className="item" key={idx}>
          {" - "}{btn}
        </span>
      );
    });
  };

  return (
    <div className="info">
      <a href={'/e/'+entry.id} className="permalink">{entry.date}</a>
      {via}
      {infos}
    </div>
  );
}

class EntryCommandLike extends React.Component{

  handleLike = (event) => {
    event.preventDefault();
    this.props.onLike();
  }

  handleUnlike = (event) => {
    event.preventDefault();
    this.props.onUnlike();
  }

  render() {
    if (this.props.liked) {
      return (
        <a href="#nolink" onClick={this.handleUnlike}>
          Unlike
        </a>
      );
    } else {
      return (
        <a href="#nolink" onClick={this.handleLike}>
          Like
        </a>
      );
    }
  }
}

class EntryCommandComment extends React.Component{

  handleClick = (event) => {
    event.preventDefault();
    this.props.onNewComment(this);
  }

  render() {
    return (
      <a href="#nolink" onClick={this.handleClick}>Comment</a>
    );
  }
}

class EntryCommandEdit extends React.Component{

  handleClick = (event) => {
    event.preventDefault();
    this.props.onEdit(event);
  }

  render() {
    return (
      <a href="#nolink" className="editcommand" onClick={this.handleClick}>Edit</a>
    );
  }
}

class EntryCommandDelete extends React.Component{

  handleClick = (event) => {
    event.preventDefault();
    this.props.onDelete(this);
  }

  render() {
    return (
      <a href="#nolink" className="deletecommand" onClick={this.handleClick}>Delete</a>
    );
  }
}

class EntryCommentForm extends React.Component{

  constructor(props) {
    super(props);
    this.state = props;
    this.state.value = props.commentBody;
  }

  handleChange = (event) => {
    this.setState({value: event.target.value});
  }

  onSubmitComment = (event) =>  {
    event.preventDefault();
    if (!this.state.value) {
      return;
    }
    this.props.onSubmitComment(this.props.commentId, this.state.value, event);
    this.setState({value: ''});
  }

  onCancelComment = (event) => {
    event.preventDefault();
    var comment = this.state.value;
    this.props.onCancelComment(this.props.commentId, comment, event);
  }

  render() {
    return (
          <div className="comment form">
          <form method="post">
            <textarea autoFocus name="body" ref="commentInput"
                      onChange={this.handleChange}
                      value={this.state.value} />
            <input type="submit" value="Post"
                   onClick={this.onSubmitComment} />
            <span onClick={this.onCancelComment}>Cancel</span>
          </form>
          </div>
    );
  }
}

class EntryLike extends React.Component{

  getInitialState() {
    return {expanded: false};
  }

  expandLikes= (event) => {
    if (this.state.expanded) {
      return;
    }

    event.preventDefault();
    this.props.expandLikes();
    this.setState({expanded: true});
  }

  render() {
    var like = this.props.like;
    if (like.placeholder) {
      return (
        <a href="#nolink" onClick={this.expandLikes}>{like.body}</a>
      );
    } else {
      return (
        <a href={'/feed/' + like.from.id }>
          {like.from.name}
        </a>
      );
    }
  }
}

function EntryLikes(props) {
  if (!props.likes || props.likes.length === 0) {
    return null;
  }

  var expandLikes = props.expandLikes;
  var likes = props.likes.map(function(like, index) {
    return (
      <EntryLike like={like} key={index}
                 expandLikes={expandLikes} />
    );
  });
  if (likes.length === 1) {
    return (
      <div className="likes">
        {likes}{" liked this"}
      </div>
    );
  }

  var last = likes[likes.length-1];
  likes = likes.slice(0, -1);
  likes = intersperse(likes, ", ");

  return (
    <div className="likes">
      {likes}{" and "}{last}{" liked this"}
    </div>
  );
}

class EntryComment extends React.Component{

  constructor(props) {
    super(props);
    this.state = props;
  }

  static getDerivedStateFromProps(nextProps, state) {
    if (state.comment !== nextProps.comment) {
      return {comment: nextProps.comment}
    }
    return null;
  }

  expandComments = (event) => {
    event.preventDefault();
    this.props.expandComments();
  }

  editComment = (event) => {
    event.preventDefault();
    this.props.editComment(this.state.comment);
    // this.setState({comment: this.state.comment});
  }

  deleteComment = (event) => {
    event.preventDefault();
    var comment = this.props.deleteComment(this.state.comment);
    this.setState({comment: comment});
  }

  render() {
    var comment = this.state.comment;

    if (!comment) {
      return (
        <div className="comment placeholder">
          <span>Comment deleted.</span>
        </div>
      );
    }

    var cmds = null
    if (comment.commands && comment.commands.length > 0) {
      cmds = (
        <span className="commands">
          {" ( "}
          <a href="#nolink" onClick={this.editComment}>Edit</a>
          {" | "}
          <a href="#nolink" onClick={this.deleteComment}>Delete</a>
          {" )"}
        </span>
      );
    }

    if (comment.placeholder) {
      return (
        <div className="comment placeholder">
          <a href="#nolink" onClick={this.expandComments}>{comment.body}</a>
        </div>
      );
    } else {
      var body = `${comment.body} - <a href="/feed/${comment.from.id}">${comment.from.name}</a>`;
      return (
        <div onFocus={this.showCommands}
             className="comment" title={comment.date}>
          <span dangerouslySetInnerHTML={{__html: body}}></span>
          {cmds}
        </div>
      );
    }
  }
}
