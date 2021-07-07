import logo from './logo.svg';
import './App.css';
import React from 'react';


function dprint(msg) {
  if (typeof window !== 'undefined' && window.console && window.console.log) {
    window.console.log(msg);
  }
}

function getJSON(url) {
  return fetch(url, {
    cache: 'no-cache',
    credentials: 'same-origin', // include, same-origin, *omit
    headers: {
      'user-agent': 'Mozilla/4.0 MDN',
      'content-type': 'application/json'
    },
    method: 'GET',
    mode: 'cors', // no-cors, cors, *same-origin
    redirect: 'follow',
    referrer: 'no-referrer',
  })
  .then(response => response.json()) // parses response to JSON
}

function postJSON(url, data) {
  return fetch(url, {
    body: JSON.stringify(data),
    cache: 'no-cache',
    credentials: 'same-origin', // include, same-origin, *omit
    headers: {
      'user-agent': 'Mozilla/4.0 MDN',
      'content-type': 'application/json'
    },
    method: 'POST', // *GET, POST, PUT, DELETE, etc.
    mode: 'cors', // no-cors, cors, *same-origin
    redirect: 'follow', // manual, *follow, error
    referrer: 'no-referrer', // *client, no-referrer
  })
  .then(response => response.json()) // parses response to JSON
}

/* intersperse: Return an array with the separator interspersed between
 * each element of the input array.
 *
 * > _([1,2,3]).intersperse(0)
 * [1,0,2,0,3]
 */
function intersperse(arr, sep) {
    if (arr.length === 0) {
        return [];
    }

    return arr.slice(1).reduce(function(xs, x, i) {
        return xs.concat([sep, x]);
    }, [arr[0]]);
}

class Entry extends React.Component {

  getInitialState() {
    // var comments = this.props.entry.comments
    return {
      entry: this.props.entry,
      comments: this.props.entry.comments,
      likes: this.props.entry.likes,
      new_comment_form: false,
      expanded_likes: false,
      expanded_comments: false,
      comment_preserve: null
    };
  }

  UNSAFE_componentWillReceiveProps(nextProps){
    var newdata = {
      entry: nextProps.entry,
    }
    if (!this.state.expanded_comments && nextProps.entry.comments) {
      var safe_update = true;
      var comments = this.state.comments || [];
      comments.forEach(function(cmt) {
        if (cmt.is_editing) {
          safe_update = false;
        }
      });
      if (safe_update) {
        newdata.comments = nextProps.entry.comments;
      }
    }
    if (!this.state.expanded_likes) {
      newdata.likes = nextProps.entry.likes;
    }
    this.setState(newdata);
  }

  // handleEdit: function(child) {
  //   console.log("edit entry")
  // },

  handleDelete(child) {
    console.log("handle delete");
    var entry = this.state.entry;
    postJSON("/a/delete", {entry: entry.id})
      .then(function(data) {
        console.log(data);
        console.log("entry deleted... how to do?");
      });
  }

  handleNewComment(child) {
    if (this.state.new_comment_form) {
      // focus
      React.findDOMNode(this.refs.commentInput).focus();
    } else {
      // make form
      this.setState({new_comment_form: true});
    }
  }

  submitComment(event, id, comment) {
    event.preventDefault();
    var self = this;
    var comments = this.state.comments || [];
    var args = {
      entry: this.props.entry.id,
      body: comment
    };
    if (id) {
      args.id = id;
    }
    postJSON("/a/comment", args)
      .then(function(comment) {
        if (id) {
          var cmts = comments.map(function(cmt, index) {
            if (id === cmt.id) {
              return comment;
            }
            return cmt;
          });
          self.setState({comments: cmts});
        } else {
          comments.push(comment);
          self.setState({
            comments: comments,
            new_comment_form: false
          });
        }
      });
  }

  cancelComment(id, body) {
    if (id) {
      var comments = [];
      this.state.comments.forEach(function(cmt, index) {
        if (id === cmt.id) {
          cmt.is_editing = false;
        }
        comments.push(cmt);
      });
      this.setState({comments: comments});
    } else {
      if (body) {
        this.setState({comment_preserve: body});
      }
      this.setState({new_comment_form: false});
    }
  }

  expandComments(event) {
    var self = this;
    getJSON("/a/entry/" + this.props.entry.id)
      .then(function(data) {
        self.setState({
          expanded_comments: true,
          comments: data
        });
      });
  }

  expandLikes() {
    var self = this;
    getJSON("/a/expandlikes/" + this.props.entry.id)
      .then(function(data) {
        self.setState({
          expanded_likes: true,
          likes: data
        });
      });
  }

  editComment(comment) {
    var comments = [];
    this.state.comments.forEach(function(cmt, index) {
      if (comment.id && comment.id === cmt.id) {
        cmt.is_editing = true;
      }
      comments.push(cmt);
    });
    this.setState({comments: comments});
  }

  deleteComment(comment) {
    if (!comment.id) {
      return comment;
    }
    var data = {entry: this.state.entry.id, comment: comment.id}
    postJSON("/a/comment/delete", data)
      .then(function(data) {
        comment.body = "comment deleted";
      });
    return null;
  }

  handleLike() {
    var self = this;
    var entry = this.state.entry;
    postJSON("/a/like", {entry: entry.id})
      .then(function(likes) {
        entry.commands.forEach(function(cmd, index) {
          if (cmd === "like") {
            entry.commands[index] = "unlike";
          }
        });
        self.setState({likes: likes});
      });
  }

  handleUnlike() {
    var self = this;
    var entry = this.state.entry;
    postJSON("/a/like/delete", {entry: entry.id})
      .then(function(likes) {
        entry.commands.forEach(function(cmd, index) {
          if (cmd === "unlike") {
            entry.commands[index] = "like";
          }
        });
        self.setState({likes: likes});
      });
  }

  render() {
    var entry = this.state.entry;

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
        <EntryPicture feed={entry.from} />
        <div className="body">
          <EntryAuthor from={entry.from} to={entry.to} />
          <EntryTitle body={entry.body} />
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

function EntryTitle(props) {
  return (
    <div className="title" dangerouslySetInnerHTML={{__html: props.body}}>
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
    var self = this;
    infos = entry.commands.map(function(cmd, idx) {
      var btn = null
      var liked = false;
      switch (cmd) {
        case "comment":
          btn = <EntryCommandComment onNewComment={self.props.onNewComment} />;
          break;
        case "like":
          btn = <EntryCommandLike onLike={self.props.onLike} liked={liked} />;
          break;
        case "unlike":
          liked = true;
          btn = <EntryCommandLike onUnlike={self.props.onUnlike} liked={liked} />;
          break;
        case "edit":
          btn = <EntryCommandEdit />;
          break;
        case "delete":
          btn = <EntryCommandDelete onDelete={self.props.onDelete} />;
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

  handleLike(event) {
    event.preventDefault();
    this.props.onLike();
  }

  handleUnlike(event) {
    event.preventDefault();
    this.props.onUnlike();
  }

  render() {
    if (this.props.liked) {
      return (
        <a href="#nofollow" onClick={this.handleUnlike}>
          Unlike
        </a>
      );
    } else {
      return (
        <a href="#nofollow" onClick={this.handleLike}>
          Like
        </a>
      );
    }
  }
}

class EntryCommandComment extends React.Component{

  handleClick(event) {
    event.preventDefault();
    this.props.onNewComment(this);
  }

  render() {
    return (
      <a href="#nofollow" onClick={this.handleClick}>Comment</a>
    );
  }
}

class EntryCommandEdit extends React.Component{

  handleClick(event) {
    event.preventDefault();
    console.log("entry command edit")
  }

  render() {
    return (
      <a href="#nofollow" className="editcommand" onClick={this.handleClick}>Edit</a>
    );
  }
}

class EntryCommandDelete extends React.Component{

  handleClick(event) {
    event.preventDefault();
    console.log("entry command delete")
    this.props.onDelete(this);
  }

  render() {
    return (
      <a href="#nofollow" className="deletecommand" onClick={this.handleClick}>Delete</a>
    );
  }
}

class EntryCommentForm extends React.Component{

  getInitialState() {
    return {value: this.props.commentBody};
  }

  handleChange(event) {
    this.setState({value: event.target.value});
  }

  onSubmitComment(event) {
    event.preventDefault();
    if (!this.state.value) {
      return;
    }
    this.props.onSubmitComment(this.props.commentId, this.state.value);
    this.setState({value: ''});
  }

  onCancelComment(event) {
    event.preventDefault();
    var comment = this.state.value;
    this.props.onCancelComment(this.props.commentId, comment);
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

  expandLikes(event) {
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
        <a href="#nofollow" onClick={this.expandLikes}>{like.body}</a>
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

  getInitialState() {
    return {comment: this.props.comment};
  }

  UNSAFE_componentWillReceiveProps(nextProps){
    this.setState({comment: nextProps.comment});
  }

  expandComments(event) {
    event.preventDefault();
    this.props.expandComments();
  }

  editComment(event) {
    event.preventDefault();
    this.props.editComment(this.state.comment);
    // this.setState({comment: comment});
  }

  deleteComment(event) {
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
          <a href="#nofollow" onClick={this.editComment}>Edit</a>
          {" | "}
          <a href="#nofollow" onClick={this.deleteComment}>Delete</a>
          {" )"}
        </span>
      );
    }

    if (comment.placeholder) {
      return (
        <div className="comment placeholder">
          <a href="#nofollow" onClick={this.expandComments}>{comment.body}</a>
        </div>
      );
    } else {
      var body = `${comment.body} - <a href="/feed/${comment.from.id}">${comment.from.name}</a>"`;
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

function FeedPagin(props) {
  var prev = null;
  var next = null;
  var sep = null;
  if (props.show) {
    if (props.next > 30) {
      prev = <a href={'?start='+props.prev}>&laquo; Prev</a>;
      sep = " ";
    }
    next = <a href={'?start='+props.next}>Next &raquo;</a>;
  }
  return (
    <div className="pager bottom">
      {prev}{sep}{next}
    </div>
  );
}

export class Feed extends React.Component{

  refreshInterval = 30 * 1000

  loadFeeds() {
    getJSON(this.props.url)
      .then(function(data) {
        this.setState(data);
      })
      .catch(error => console.error(error))
      .bind(this)
  }

  // Set the initial component state
  getInitialState(props){
    return props || this.props;
  }

  UNSAFE_componentWillReceiveProps(nextProps){
    dprint("componentWillReceiveProps");
    this.setState(this.getInitialState(nextProps));
  }

  componentDidMount() {
    if (typeof window === 'undefined') {
      return;
    }
    if (window.app_props) {
      dprint("Loading feeds...");
      this.setState(window.app_props);
    } else {
      dprint("Fetching feeds...");
      this.loadFeeds();
    }
    setInterval(this.loadFeeds, this.refreshInterval);
  }

  render() {
    if (!this.state.feed || !this.state.feed.entries) {
      return null;
    }

    var feed = this.state.feed;
    var entryNodes = feed.entries.map(function(entry, index){
      return (
        <Entry entry={entry} key={entry.id}>
        </Entry>
      );
    });

    return (
      <div className="feed">
        {entryNodes}
        <FeedPagin show={this.state.show_paging} prev={this.state.prev_start}
                   next={this.state.next_start} />
      </div>
    );
  }
}

function App() {
  return (
    <div className="App">
      <header className="App-header">
        <img src={logo} className="App-logo" alt="logo" />
        <p>
          编辑 <code>src/App.js</code> 重新载入.
        </p>
        <a
          className="App-link"
          href="https://reactjs.org"
          target="_blank"
          rel="noopener noreferrer"
        >
          Learn React
        </a>
      </header>
    </div>
  );
}

export default App;
