// @ts-check
import React, {useState} from 'react';
import {confirmationActions, confirmationPopover, obsidianButton, outlinedButton} from './button-styles';
import {GroupManagementNav} from './group-management';

const activeNav = 'border-b-2 border-primary px-3 py-2 text-sm font-medium text-primary';
const inactiveNav = 'border-b-2 border-transparent px-3 py-2 text-sm text-muted-foreground hover:text-foreground';

/** @param {{feedId: string}} props */
function PersonalFeedApiNav({feedId}) {
  return <nav className="mb-6 flex gap-1 border-b border-border" aria-label="Feed management">
    <a href={`/feed/${feedId}`} className={inactiveNav}>Feed</a>
    <a href={`/feed/${feedId}/api`} aria-current="page" className={activeNav}>API</a>
  </nav>;
}

/** @param {number | undefined} value */
function formattedTime(value) {
  return value ? new Date(value).toLocaleString() : '—';
}

/** @param {{data: import('./browser-types').FeedApiKeyPageData}} props */
export default function FeedApiKeyPage({data}) {
  const [keyStatus, setKeyStatus] = useState(data.status);
  const [token, setToken] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  /** @param {'generate' | 'rotate' | 'revoke'} action */
  async function mutate(action) {
    setBusy(true);
    setError('');
    try {
      const response = await fetch(`/feed/${encodeURIComponent(data.feed.id)}/api/${action}`, {
        method: 'POST', credentials: 'same-origin', headers: {'Accept': 'application/json'},
      });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || 'Feed API key operation failed');
      setKeyStatus(result.status);
      setToken(result.token || '');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Feed API key operation failed');
    } finally {
      setBusy(false);
    }
  }

  async function copyToken() {
    try {
      await navigator.clipboard.writeText(token);
    } catch {
      setError('Unable to copy the token. Select and copy it manually.');
    }
  }

  const nav = data.feed.type === 'group'
    ? <GroupManagementNav groupId={data.feed.id} active="api" />
    : <PersonalFeedApiNav feedId={data.feed.id} />;

  return <div className="feed">
    <h2 className="page-title">API access: {data.feed.name}</h2>
    {nav}
    {error && <div className="error-banner" role="alert">{error}</div>}
    <p className="hint">This key represents the Feed itself. Treat it like a password.</p>

    <dl className="item-list">
      <div><dt>Status</dt><dd>{keyStatus.active ? 'Active' : keyStatus.revoked_at_ms ? 'Revoked' : 'Not generated'}</dd></div>
      <div><dt>Key ID</dt><dd>{keyStatus.key_id || '—'}</dd></div>
      <div><dt>Created</dt><dd>{formattedTime(keyStatus.created_at_ms)}</dd></div>
      <div><dt>Rotated</dt><dd>{formattedTime(keyStatus.rotated_at_ms)}</dd></div>
      <div><dt>Revoked</dt><dd>{formattedTime(keyStatus.revoked_at_ms)}</dd></div>
    </dl>

    {token && <div className="notification" role="status">
      <strong>Copy this token now. It will not be shown again.</strong>
      <div className="mt-2 break-all"><code>{token}</code></div>
      <button type="button" className={`${outlinedButton} mt-3`} onClick={copyToken}>Copy</button>
    </div>}

    <div className="mt-6 flex gap-2">
      {!keyStatus.active && !keyStatus.revoked_at_ms && <>
        <button type="button" className={obsidianButton} popoverTarget="generate-feed-api-key">Generate</button>
        <div id="generate-feed-api-key" className={confirmationPopover} popover="auto">
          <p><strong>Generate an API key?</strong></p>
          <p className="hint">The complete token is displayed once.</p>
          <div className={confirmationActions}>
            <button type="button" disabled={busy} className={obsidianButton} onClick={() => mutate('generate')}
                    popoverTarget="generate-feed-api-key" popoverTargetAction="hide">Generate</button>
            <button type="button" className={outlinedButton} popoverTarget="generate-feed-api-key" popoverTargetAction="hide">Cancel</button>
          </div>
        </div>
      </>}
      {keyStatus.active && <>
        <button type="button" className={outlinedButton} popoverTarget="rotate-feed-api-key">Rotate</button>
        <button type="button" className={obsidianButton} popoverTarget="revoke-feed-api-key">Revoke</button>
        <div id="rotate-feed-api-key" className={confirmationPopover} popover="auto">
          <p><strong>Rotate this API key?</strong></p>
          <p className="hint">The current token stops working immediately.</p>
          <div className={confirmationActions}>
            <button type="button" disabled={busy} className={obsidianButton} onClick={() => mutate('rotate')}
                    popoverTarget="rotate-feed-api-key" popoverTargetAction="hide">Rotate</button>
            <button type="button" className={outlinedButton} popoverTarget="rotate-feed-api-key" popoverTargetAction="hide">Cancel</button>
          </div>
        </div>
        <div id="revoke-feed-api-key" className={confirmationPopover} popover="auto">
          <p><strong>Revoke this API key?</strong></p>
          <p className="hint">API access stops immediately.</p>
          <div className={confirmationActions}>
            <button type="button" disabled={busy} className={obsidianButton} onClick={() => mutate('revoke')}
                    popoverTarget="revoke-feed-api-key" popoverTargetAction="hide">Revoke</button>
            <button type="button" className={outlinedButton} popoverTarget="revoke-feed-api-key" popoverTargetAction="hide">Cancel</button>
          </div>
        </div>
      </>}
      {!keyStatus.active && keyStatus.revoked_at_ms && <button type="button" disabled={busy} className={obsidianButton} onClick={() => mutate('generate')}>Generate new key</button>}
    </div>
  </div>;
}
