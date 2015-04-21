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

    return (
      <div className="entry" data-eid={entry.id}>
        <EntryPicture feed={entry.from} />
        <div className="body">
          <EntryAuthor from={entry.from} to={entry.to} />
          <EntryTitle body={entry.body} />
          {medias}
          <EntryInfo entry={entry} />
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
      <span className="to">to
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
    var via = null;
    if (entry.via) {
      via = "from " + <a href={entry.via.url} className='via'>{entry.via.name}</a>;
    }

    var commands = null;
    if (entry.commands) {
      commands = entry.commands.map(function(cmd, idx) {
        return <EntryCommand command={cmd} key={idx} />
      });
    };

    return (
      <div className="info">
        <a href={'/e/'+entry.id} className="permalink">{entry.date}</a>
        {via}
        {commands}
      </div>
    );
  }
});

var EntryCommand = React.createClass({
  render: function() {
    return (
      "- " + <a href="#" className={this.props.command + 'command'}>{this.props.command}</a>
    );
  }
});


var Feed = React.createClass({

  getInitialState() {
    return {
      message: "loading..."
    };
  },

  componentDidMount() {
    this.setState({ message: "welcome!" });
  },

  render() {
    var entryNodes = this.props.data.map(function(entry, index) {
      return (
        <Entry entry={entry} key={index}>
        </Entry>
      );
    });

    return (
      <div className="feed">
        <p>server-side rendering sample</p>
        <p>{this.state.message}</p>

        {entryNodes}
      </div>
    );
  }
});

self.Feed = Feed;
