// @ts-check
import React from 'react';

/** @param {{data: import('./browser-types').RequestsPageData}} props */
export function RequestsPage({data}) {
  return <div className="feed">
    <h2 className="page-title">Follow requests</h2>
    {data.error && <div className="error-banner" role="alert">{data.error}</div>}
    {!data.private && <p className="muted">Your feed is public — anyone can follow it without approval. Requests only apply while your feed is private.</p>}
    <ul className="item-list">
      {data.requests.length === 0
        ? <li className="muted">No pending requests.</li>
        : data.requests.map(request => <li key={request.requester.uuid}>
            <img src={request.requester.picture} className="avatar" alt="" />
            <a href={`/feed/${request.requester.id}`}>{request.requester.name}</a>
            {request.requester.private && <span className="private-icon" role="img" aria-label="Private" title="Private" />}
            <span className="item-time">{request.requested_at}</span>
            <form method="post" action="/account/requests/action">
              <input type="hidden" name="target_uuid" value={request.requester.uuid} />
              <button type="submit" name="action" value="approve" className="legacy-button">Approve</button>
              <button type="submit" name="action" value="reject" className="legacy-button danger">Reject</button>
            </form>
          </li>)}
    </ul>
  </div>;
}
