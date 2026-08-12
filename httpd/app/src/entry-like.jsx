// @ts-check

import React from 'react';

/**
 * @typedef {object} EntryLikeProps
 * @property {{placeholder?: boolean, body?: string, from?: {id?: string, name?: string}}} like
 * @property {() => void} expandLikes
 *
 * @typedef {{expanded: boolean}} EntryLikeState
 */

/** @extends {React.Component<EntryLikeProps, EntryLikeState>} */
export class EntryLike extends React.Component {
  /** @param {EntryLikeProps} props */
  constructor(props) {
    super(props);
    this.state = {expanded: false};
  }

  /** @param {React.SyntheticEvent} event */
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
        <button type="button" className="inline-action action-link"
                onClick={this.expandLikes}>{like.body}</button>
      );
    }

    const actor = like.from;
    const name = actor?.name || actor?.id || 'Unknown';
    if (!actor?.id) {
      return <span>{name}</span>;
    }

    return (
      <a href={'/feed/' + actor.id}>
        {name}
      </a>
    );
  }
}
