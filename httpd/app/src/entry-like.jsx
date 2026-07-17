import React from 'react';

export class EntryLike extends React.Component {
  constructor(props) {
    super(props);
    this.state = {expanded: false};
  }

  expandLikes = (event) => {
    event.preventDefault();
    if (this.state.expanded) {
      return;
    }

    this.props.expandLikes();
    this.setState({expanded: true});
  }

  render() {
    const like = this.props.like;
    if (like.placeholder) {
      return (
        <a href="#nolink" onClick={this.expandLikes}>{like.body}</a>
      );
    }

    return (
      <a href={'/feed/' + like.from.id}>
        {like.from.name}
      </a>
    );
  }
}
