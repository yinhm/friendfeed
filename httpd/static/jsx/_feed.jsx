'use strict';
var React = self.React;

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

var Entry = React.createClass({
  render: function() {
    var entry = this.props.entry;

    var medias = "";
    if (entry.thumbnails) {
      medias = <EntryMediaBox thumbs={entry.thumbnails} />
    }

    var comments = null;
    if (entry.comments) {
      var comments = entry.comments.map(function(comment, index) {
        return (
          <EntryComment comment={comment} key={index} />
        );
      });
    }

    return (
      <div className="entry" data-eid={entry.id}>
        <EntryPicture feed={entry.from} />
        <div className="body">
          <EntryAuthor from={entry.from} to={entry.to} />
          <EntryTitle body={entry.body} />
          {medias}
          <EntryInfo entry={entry} />
          <EntryLikes likes={entry.likes} />
          {comments}
        </div>
      </div>
    );
  }
});

var EntryPicture = React.createClass({
  render: function() {
    var feed = this.props.feed;
    return (
      <div className="picture">
        <a href={'/feed/'+feed.id}>
          <img src={feed.picture} /></a>
      </div>
    );
  }
});

var EntryToFeeds = React.createClass({
  render: function() {
    var comma  = ", ";
    var length = this.props.feeds.length - 1;
    var feeds = this.props.feeds.map(function(feed, index) {
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
});

var EntryToFeed = React.createClass({
  render: function() {
    return (
      <a href={'/feed/' + this.props.feed.id}>{this.props.feed.name}</a>
    );
  }
});

var EntryAuthor = React.createClass({
  render: function() {
    var from = this.props.from;

    var toFeeds;
    if (this.props.to) {
      toFeeds = <EntryToFeeds feeds={this.props.to} />;
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
});

var EntryMedia = React.createClass({
  render: function() {
    var thumb = this.props.thumb;
    var style = "";
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
});

var EntryMediaBox = React.createClass({
  render: function() {
    var medias = this.props.thumbs.map(function(thumb, index) {
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
});

var EntryTitle = React.createClass({
  render: function() {
    return (
      <div className="title" dangerouslySetInnerHTML={{__html: this.props.body}}>
      </div>
    );
  }
});

var EntryInfo = React.createClass({
  render: function() {
    var entry = this.props.entry;
    var info;
    var infos = [];
    if (entry.via) {
      info = <span className="item">
        {" from "}<a href={entry.via.url} className='via'>{entry.via.name}</a>
      </span>;
      infos.push(info);
    }

    if (entry.commands) {
      entry.commands.map(function(cmd, idx) {
        info = <span className="item" key={idx}>
        {" - "}<EntryCommand command={cmd} /></span>;
        infos.push(info);
      });
    };

    return (
      <div className="info">
        <a href={'/e/'+entry.id} className="permalink">{entry.date}</a>
        {infos}
      </div>
    );
  }
});

var EntryCommand = React.createClass({
  render: function() {
    return (
      <a href="#" className={this.props.command + 'command'}>{this.props.command}</a>
    );
  }
});


var EntryLike = React.createClass({
  render: function() {
    var like = this.props.like;
    if (like.placeholder) {
      return (
        <span>{like.body}</span>
      );
    } else {
      return (
        <a href={'/feed/' + like.from.id }>
          {like.from.name}
        </a>
      );
    }
  }
});

var EntryLikes = React.createClass({
  render: function() {
    if (!this.props.likes || this.props.likes.length == 0) {
      return null;
    }

    var likes = this.props.likes.map(function(like, index) {
      return (
        <EntryLike like={like} key={index} />
      );
    });
    if (likes.length == 1) {
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
});

var EntryComment = React.createClass({
  render: function() {
    var comment = this.props.comment;
    if (comment.placeholder) {
      return (
        <div data-cid={comment.id} className="comment placeholder">
          <a href="#" className="expandcomments">{comment.body}</a>
        </div>
      );
    } else {
      return (
        <div data-cid={comment.id} className="comment" title={comment.date}>
          {comment.body}
          {" - "}
          <a href={'/feed/' + comment.from.id }>{comment.from.name}</a>
        </div>
      );
    }
  }
});

var Feed = React.createClass({

  refreshInterval: 20 * 1000,

  loadFeeds: function() {
    $.ajax({
      url: this.props.url,
      dataType: 'json',
      success: function(data) {
        console.log(data);
        this.setState(data);
      }.bind(this),
      error: function(xhr, status, err) {
        console.error(this.props.url, status, err.toString());
      }.bind(this)
    });
  },

  // Set the initial component state
  getInitialState: function(props){
    console.log("getInitialState");
    return props || this.props;
  },

  componentWillReceiveProps: function(nextProps){
    console.log("componentWillReceiveProps");
    this.setState(this.getInitialState(nextProps));
  },

  componentDidMount() {
    console.log("Loading feeds from server...");
    this.loadFeeds();
    setInterval(this.loadFeeds, this.refreshInterval);
  },

  render() {
    if (!this.state.feed || this.state.feed.entries.length == 0) {
      return null;
    }

    console.log("rending...");
    var feed = this.state.feed;
    var entryNodes = feed.entries.map(function(entry, index) {
      return (
        <Entry entry={entry} key={entry.id}>
        </Entry>
      );
    });

    return (
      <div className="feed">
        {entryNodes}
      </div>
    );
  }
});

self.Feed = Feed;

if (typeof window !== 'undefined') {
  React.render(
    <Feed url="/public" />,
    document.getElementById('feed')
  );
}
