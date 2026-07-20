// @ts-check

import React, {useState} from 'react';
import { EntryContent } from './content'
import {EntryLike} from './entry-like';
import {getJSON, postJSON, postForm, intersperse} from './utils';

/**
 * @typedef {{id: string, name: string, picture?: string, title?: string}} FeedRef
 * @typedef {{width?: number, height?: number, link: string, url: string}} Thumbnail
 * @typedef {{id?: string, body: string, rawBody?: string, is_editing?: boolean,
 * commands?: string[], placeholder?: boolean, from: FeedRef, date?: string}} CommentData
 * @typedef {FeedRef} LikeData
 * @typedef {{name: string, url: string}} ViaData
 * @typedef {object} EntryData
 * @property {string} id
 * @property {FeedRef} from
 * @property {FeedRef[]} [to]
 * @property {string} [title]
 * @property {string} body
 * @property {string} [rawBody]
 * @property {string} [type]
 * @property {string} [date]
 * @property {ViaData} [via]
 * @property {string[]} commands
 * @property {Thumbnail[]} [thumbnails]
 * @property {CommentData[]} [comments]
 * @property {LikeData[]} [likes]
 *
 * @typedef {{entry: EntryData, onpage_edit: boolean}} EntryProps
 * @typedef {object} EntryState
 * @property {EntryData} entry
 * @property {EntryData} source_entry
 * @property {boolean} onpage_edit
 * @property {CommentData[] | undefined} comments
 * @property {LikeData[] | undefined} likes
 * @property {boolean} new_comment_form
 * @property {boolean} expanded_likes
 * @property {boolean} expanded_comments
 * @property {boolean} is_deleted
 * @property {string | null} comment_preserve
 */

/** @param {EntryProps} nextProps @param {EntryState} state */
function mergeEntryProps(nextProps, state) {
  var newState = {...state};

  if (state.onpage_edit) {
    console.log("no update entry content when on-page editing");
  } else {
    newState.entry = nextProps.entry;
  }

  // Merge server data only while the corresponding local view is untouched.
  if (!state.expanded_comments && nextProps.entry.comments) {
    var safeUpdate = !(state.comments ?? []).some((comment) => comment.is_editing);
    if (safeUpdate) {
      newState.comments = nextProps.entry.comments;
    }
  }
  if (!state.expanded_likes) {
    newState.likes = nextProps.entry.likes;
  }
  return newState;
}

/** @param {EntryProps} props */
export function Entry(props) {
  const [state, setEntryState] = useState(/** @type {EntryState} */ ({
    entry: props.entry,
    source_entry: props.entry,
    onpage_edit: props.onpage_edit,
    comments: props.entry.comments,
    likes: props.entry.likes,
    new_comment_form: false,
    expanded_likes: false,
    expanded_comments: false,
    is_deleted: false,
    comment_preserve: null,
  }));

  if (state.source_entry !== props.entry) {
    setEntryState(current => ({
      ...mergeEntryProps(props, current),
      source_entry: props.entry,
    }));
  }

  /** @param {Partial<EntryState>} newState */
  const updateState = (newState) => {
    setEntryState(current => ({...current, ...newState}));
  };

  /** @param {React.SyntheticEvent} _event */
  const handleEdit = (_event) => {
    console.log("edit entry")
    // var onpageEdit = !this.state.onpage_edit;
    updateState({onpage_edit: true});
  };

  const handleDelete = () => {
    var entry = state.entry;
    postJSON("/a/delete", {entry: entry.id})
      .then(() => {
        updateState({is_deleted: true});
      });
  };

  const handleNewComment = () => {
    if (!state.new_comment_form) {
      // make form; the textarea focuses itself via autoFocus
      updateState({new_comment_form: true});
    }
  };

  /**
   * @param {string | undefined} id
   * @param {string} comment
   * @param {React.SyntheticEvent} event
   */
  const submitComment = (id, comment, event) => {
    event.preventDefault();
    var comments = state.comments || [];
    /** @type {{entry: string, body: string, id?: string}} */
    var args = {
      entry: props.entry.id,
      body: comment
    };
    if (id) {
      args.id = id;
    }
    return postJSON("/a/comment", args)
      .then(comment => { // arrow function
        if (id) {
          var cmts = comments.map(function(cmt) {
            if (id === cmt.id) {
              comment.is_editing = false;
              return comment;
            }
            return cmt;
          });
          updateState({comments: cmts});
        } else {
          comments.push(comment);
          updateState({
            comments: comments,
            new_comment_form: false
          });
        }
      });
  };

  /**
   * @param {string | undefined} id
   * @param {string} body
   * @param {React.SyntheticEvent} _event
   */
  const cancelComment = (id, body, _event) => {
    if (id) {
      /** @type {CommentData[]} */
      var comments = [];
      (state.comments ?? []).forEach(function(cmt) {
        if (id === cmt.id) {
          cmt.is_editing = false;
        }
        comments.push(cmt);
      });
      updateState({comments: comments});
    } else {
      if (body) {
        updateState({comment_preserve: body});
      }
      updateState({new_comment_form: false});
    }
  };

  const expandComments = () => {
    getJSON("/a/entry/" + props.entry.id)
      .then(data => { // arrow function
        updateState({
          expanded_comments: true,
          comments: data
        });
      });
  };

  const expandLikes = () => {
    getJSON("/a/expandlikes/" + props.entry.id)
      .then(data => { // arrow function
        updateState({
          expanded_likes: true,
          likes: data
        });
      });
  };

  /** @param {CommentData} comment */
  const editComment = (comment) => {
    /** @type {CommentData[]} */
    var comments = [];
    (state.comments ?? []).forEach(function(cmt) {
      if (comment.id && comment.id === cmt.id) {
        cmt.is_editing = true;
      }
      comments.push(cmt);
    });
    updateState({comments: comments});
  };

  /** @param {CommentData} comment */
  const deleteComment = (comment) => {
    if (!comment.id) {
      return comment;
    }
    var data = {entry: state.entry.id, comment: comment.id}
    postJSON("/a/comment/delete", data)
      .then(() => {
        comment.body = "comment deleted";
        setEntryState(current => ({
          ...current,
          comments: (current.comments ?? []).filter(
            (currentComment) => comment.id !== currentComment.id
          ),
        }));
      });
    return null;
  };

  const handleLike = () => {
    var entry = state.entry;
    postJSON("/a/like", {entry: entry.id})
      .then(likes => { // arrow function
        entry.commands.forEach(function(cmd, index) {
          if (cmd === "like") {
            entry.commands[index] = "unlike";
          }
        });
        updateState({likes: likes});
      });
  };

  const handleUnlike = () => {
    var entry = state.entry;
    postJSON("/a/like/delete", {entry: entry.id})
      .then(likes => { // arrow function
        entry.commands.forEach(function(cmd, index) {
          if (cmd === "unlike") {
            entry.commands[index] = "like";
          }
        });
        updateState({likes: likes});
      });
  };

  /** @param {FormData} formData */
  const onPostEntry = (formData) => {
    // on post
    return postForm("/a/share", formData)
      .then(entry => {
        setEntryState(current => {
          if (current.entry.id !== entry.id) {
            console.log("update failed, new entry created?")
          }
          return {...current, entry, onpage_edit: false};
        });
      });
  };

    var entry = state.entry;
    var edit_mode = state.onpage_edit || false;
    var bodyClass = edit_mode? 'editBody' : 'body';

    if (state.is_deleted) {
        return (
        <div className="entry" data-eid={entry.id}>
          <div className="body">
              entry deleted.
          </div>
        </div>
        )
    }

    /** @type {React.ReactNode} */
    var medias = null;
    if (entry.thumbnails) {
      medias = <EntryMediaBox thumbs={entry.thumbnails} />;
    }

    /** @type {React.ReactNode} */
    var comments = null;
    if (state.comments) {
      comments = state.comments.map(function(comment, index) {
        if (comment.is_editing) {
          return (
            <EntryCommentForm commentId={comment.id}
                              commentBody={comment.rawBody}
                              onSubmitComment={submitComment}
                              onCancelComment={cancelComment}/>
          );
        } else {
          return (
            <EntryComment comment={comment}
                          expandComments={expandComments}
                          editComment={editComment}
                          deleteComment={deleteComment}
                          key={index} />
          );
        }
      });
    }

    var form_cmt = null;
    if (state.new_comment_form) {
      form_cmt = <EntryCommentForm commentBody={state.comment_preserve}
                                   onSubmitComment={submitComment}
                                   onCancelComment={cancelComment}/>

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
            title={entry.title}
            body={entry.body}
            rawBody={entry.rawBody}
            type={entry.type}
            onpageEdit={edit_mode}
            onPostEntry={onPostEntry} />
          {medias}
          <EntryInfo entry={entry}
                     onNewComment={handleNewComment}
                     onLike={handleLike}
                     onUnlike={handleUnlike}
                     onEdit={handleEdit}
                     onDelete={handleDelete}/>
          <EntryLikes likes={state.likes}
                      expandLikes={expandLikes} />
          {comments}
          {form_cmt}
        </div>
      </div>
    );
}

/** @param {{feed: FeedRef}} props */
function EntryPicture(props) {
  var feed = props.feed;
  return (
    <div className="picture">
      <a href={'/feed/'+feed.id}>
        <img src={feed.picture} alt={feed.title} /></a>
    </div>
  );
}

/** @param {{feeds: FeedRef[]}} props */
function EntryToFeeds(props) {
  /** @type {React.ReactNode[]} */
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

/** @param {{feed: FeedRef}} props */
function EntryToFeed(props) {
  return <a href={'/feed/' + props.feed.id}>{props.feed.name}</a>;
}

/** @param {{from: FeedRef, to?: FeedRef[]}} props */
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

/** @param {{thumb: Thumbnail}} props */
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

/** @param {{thumbs: Thumbnail[]}} props */
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

/**
 * @param {{entry: EntryData, onNewComment: () => void, onLike: () => void,
 * onUnlike: () => void, onEdit: (event: React.SyntheticEvent) => void,
 * onDelete: () => void}} props
 */
function EntryInfo(props) {
  var entry = props.entry;
  /** @type {React.ReactNode[]} */
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

/**
 * @typedef {{liked: boolean, onLike?: () => void, onUnlike?: () => void}} LikeCommandProps
 * @extends {React.Component<LikeCommandProps>}
 */
class EntryCommandLike extends React.Component{

  /** @param {React.MouseEvent<HTMLAnchorElement>} event */
  handleLike = (event) => {
    event.preventDefault();
    this.props.onLike?.();
  }

  /** @param {React.MouseEvent<HTMLAnchorElement>} event */
  handleUnlike = (event) => {
    event.preventDefault();
    this.props.onUnlike?.();
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

/** @extends {React.Component<{onNewComment: (source?: unknown) => void}>} */
class EntryCommandComment extends React.Component{

  /** @param {React.MouseEvent<HTMLAnchorElement>} event */
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

/** @extends {React.Component<{onEdit: (event: React.SyntheticEvent) => void}>} */
class EntryCommandEdit extends React.Component{

  /** @param {React.MouseEvent<HTMLAnchorElement>} event */
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

/** @extends {React.Component<{onDelete: (source?: unknown) => void}, {isClicked: boolean}>} */
class EntryCommandDelete extends React.Component{

  /** @param {{onDelete: (source?: unknown) => void}} props */
  constructor(props) {
    super(props);
    this.state = { isClicked: false }
  }

  /** @param {React.MouseEvent<HTMLAnchorElement>} event */
  handleClick = (event) => {
    event.preventDefault();
    this.setState({isClicked:true}); 
  }

  /** @param {React.MouseEvent<HTMLAnchorElement>} event */
  handleDelete = (event) => {
    event.preventDefault();
    this.props.onDelete(this);
    this.setState({isClicked:false}); 
  }

  /** @param {React.MouseEvent<HTMLSpanElement>} event */
  handleCancel = (event) => {
    event.preventDefault();
    this.setState({isClicked:false}); 
  }

  render() {
    if (this.state.isClicked) {
      return (
        <>
        Confirm Delete 
        <span className="item deletecommand" onClick={this.handleCancel}> 取消 </span>
         / 
        <a href="#nolink" className="deletecommand" onClick={this.handleDelete}> 确定 </a>
        </>
      );
    }
    return (
      <a href="#nolink" className="deletecommand" onClick={this.handleClick}>Delete</a>
    );
  }
}

/**
 * @typedef {{commentId?: string, commentBody?: string | null,
 * onSubmitComment: (id: string | undefined, body: string, event: React.SyntheticEvent) => void,
 * onCancelComment: (id: string | undefined, body: string, event: React.SyntheticEvent) => void}} CommentFormProps
 * @typedef {CommentFormProps & {value: string}} CommentFormState
 * @extends {React.Component<CommentFormProps, CommentFormState>}
 */
class EntryCommentForm extends React.Component{

  /** @param {CommentFormProps} props */
  constructor(props) {
    super(props);
    this.state = {...props, value: props.commentBody ?? ''};
  }

  /** @param {React.ChangeEvent<HTMLTextAreaElement>} event */
  handleChange = (event) => {
    this.setState({value: event.target.value});
  }

  /** @param {React.MouseEvent<HTMLInputElement>} event */
  onSubmitComment = (event) =>  {
    event.preventDefault();
    if (!this.state.value) {
      return;
    }
    this.props.onSubmitComment(this.props.commentId, this.state.value, event);
    this.setState({value: ''});
  }

  /** @param {React.MouseEvent<HTMLSpanElement>} event */
  onCancelComment = (event) => {
    event.preventDefault();
    var comment = this.state.value;
    this.props.onCancelComment(this.props.commentId, comment, event);
  }

  render() {
    return (
          <div className="comment form">
          <form method="post">
            <textarea autoFocus name="body"
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

/** @param {{likes?: LikeData[], expandLikes: () => void}} props */
function EntryLikes(props) {
  if (!props.likes || props.likes.length === 0) {
    return null;
  }

  var expandLikes = props.expandLikes;
  /** @type {React.ReactNode[]} */
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

/**
 * @typedef {{comment: CommentData, expandComments: () => void,
 * editComment: (comment: CommentData) => void,
 * deleteComment: (comment: CommentData) => CommentData | null}} EntryCommentProps
 * @extends {React.Component<EntryCommentProps, {comment: CommentData | null}>}
 */
class EntryComment extends React.Component{

  /** @param {EntryCommentProps} props */
  constructor(props) {
    super(props);
    this.state = props;
  }

  /** @param {EntryCommentProps} nextProps @param {{comment: CommentData | null}} state */
  static getDerivedStateFromProps(nextProps, state) {
    if (state.comment !== nextProps.comment) {
      return {comment: nextProps.comment}
    }
    return null;
  }

  /** @param {React.MouseEvent<HTMLAnchorElement>} event */
  expandComments = (event) => {
    event.preventDefault();
    this.props.expandComments();
  }

  /** @param {React.MouseEvent<HTMLAnchorElement>} event */
  editComment = (event) => {
    event.preventDefault();
    this.props.editComment(this.state.comment);
    // this.setState({comment: this.state.comment});
  }

  /** @param {React.MouseEvent<HTMLAnchorElement>} event */
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
        <div className="comment" title={comment.date}>
          <span dangerouslySetInnerHTML={{__html: body}}></span>
          {cmds}
        </div>
      );
    }
  }
}
