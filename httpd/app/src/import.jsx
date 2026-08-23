// @ts-check

import React, {useState} from 'react';
import {getJSON, postJSON} from './utils';

/**
 * @typedef {object} ServiceData
 * @property {string} id
 * @property {string} name
 * @property {string} [icon]
 * @property {string} [profile]
 * @property {string} [username]
 * @property {string} [kind]
 * @property {string} [service_uuid]
 * @property {boolean} [enabled]
 */

/**
 * @typedef {object} ServiceState
 * @property {number} [last_fetch_ms]
 * @property {number} [next_fetch_ms]
 * @property {string} [last_error]
 * @property {string} [status]
 * @property {number} [last_success_ms]
 */

const primaryButtonClass =
  'rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50';
const smallButtonClass =
  'rounded-md border border-input bg-background px-3 py-1.5 text-xs font-medium text-foreground shadow-sm hover:bg-accent disabled:opacity-50';

/**
 * Connected import services with removal, plus entry points for adding
 * new imports (e.g. the Twitter OAuth flow).
 *
 * The services map is owned by the parent (AccountApp) so deletions
 * survive tab switches; this panel only renders and reports changes.
 *
 * @param {{services: Record<string, ServiceData>, states?: Record<string, ServiceState>, target?: string,
 * onServicesChange?: (services: Record<string, ServiceData>) => void}} props
 */
export function ImportPanel(props) {
  const [removing, setRemoving] = useState(/** @type {string | null} */ (null));
  const [error, setError] = useState(/** @type {string | null} */ (null));
  const [url, setUrl] = useState('');
  const [adding, setAdding] = useState(false);
  const [acting, setActing] = useState(/** @type {string | null} */ (null));

  const services = props.services ?? {};
  const list = Object.values(services);
  const hasTwitter = 'twitter' in services;

  /** @param {ServiceData} service */
  const handleRemove = (service) => {
    setError(null);
    setRemoving(service.id);
    const target = props.target ? `?target=${encodeURIComponent(props.target)}` : '';
    getJSON(`/account/service/${service.id}/delete${target}`)
      .then((/** @type {{deleted?: string, error?: string}} */ data) => {
        if (data && data.error) {
          setError(data.error);
          return;
        }
        const next = {...services};
        delete next[service.id];
        props.onServicesChange?.(next);
      })
      .catch((e) => setError(String(e)))
      .finally(() => setRemoving(null));
  };

  /** @param {React.FormEvent<HTMLFormElement>} event */
  const handleAdd = (event) => {
    event.preventDefault();
    setError(null);
    setAdding(true);
    postJSON('/account/feed-service', {target_uuid: props.target ?? '', url})
      .then((/** @type {ServiceData & {error?: string}} */ service) => {
        if (service.error) {
          setError(service.error);
          return;
        }
        props.onServicesChange?.({...services, [service.id]: service});
        setUrl('');
      })
      .catch((e) => setError(String(e)))
      .finally(() => setAdding(false));
  };

  /** @param {ServiceData} service */
  const serviceStatus = (service) => {
    if (service.kind !== 'web_feed') return null;
    const state = props.states?.[service.service_uuid ?? ''];
    if (!state?.last_fetch_ms) return 'Pending first fetch';
    if (state.status === 'dead') return 'Source is no longer available. Use Refresh to retry.';
    if (state.last_error) return `Last fetch failed: ${state.last_error}`;
    const timestamp = state.last_success_ms || state.last_fetch_ms;
    return `Last fetched ${new Date(timestamp).toLocaleString()}`;
  };

  /** @param {ServiceData} service */
  const serviceLabel = (service) => service.kind === 'web_feed'
    ? {type: 'RSS', title: service.name || 'Untitled feed'}
    : {type: service.name || service.id, title: ''};

  /** @param {ServiceData} service @param {'enable'|'disable'|'refresh'} action */
  const handleAction = (service, action) => {
    setError(null);
    setActing(`${service.id}:${action}`);
    postJSON(`/account/feed-service/${encodeURIComponent(service.id)}/${action}`, {
      target_uuid: props.target ?? '',
    }).then((/** @type {ServiceData & {error?: string}} */ result) => {
      if (result.error) {
        setError(result.error);
        return;
      }
      if (action !== 'refresh') props.onServicesChange?.({...services, [service.id]: result});
    }).catch((e) => setError(String(e))).finally(() => setActing(null));
  };

  return (
    <div className="max-w-xl">
      <h3 className="mb-4 text-lg font-semibold">Import Services</h3>

      {error &&
        <div role="alert" className="mb-4 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </div>}

      {list.length === 0
        ? <div className="mb-4 text-sm text-muted-foreground">No services connected yet.</div>
        : <ul className="mb-4 divide-y divide-border rounded-md border border-border">
            {list.map((service, index) => {
              const confirmationId = `remove-import-service-${index}`;
              return (
              <li key={service.id} className="flex items-center justify-between gap-3 px-3 py-2">
                <div className="text-sm">
                  <span className="font-semibold">{serviceLabel(service).type}</span>
                  {serviceLabel(service).title &&
                    <span className="ml-2 text-foreground/80">{serviceLabel(service).title}</span>}
                  {service.username &&
                    <span className="ml-2 text-muted-foreground">
                      {service.profile
                        ? <a href={service.profile} className="underline hover:text-foreground">{service.username}</a>
                        : service.username}
                    </span>}
                  {serviceStatus(service) &&
                    <div className="mt-1 text-xs text-muted-foreground">{serviceStatus(service)}</div>}
                </div>
                <div className="flex gap-1">
                  {service.kind === 'web_feed' && <>
                    <button type="button" disabled={acting !== null}
                            onClick={() => handleAction(service, service.enabled ? 'disable' : 'enable')}
                            className={smallButtonClass}>
                      {service.enabled ? 'Disable' : 'Enable'}
                    </button>
                    <button type="button" disabled={!service.enabled || acting !== null}
                            onClick={() => handleAction(service, 'refresh')}
                            className={smallButtonClass}>
                      Refresh
                    </button>
                  </>}
                  <button type="button"
                          disabled={removing === service.id}
                          popoverTarget={confirmationId}
                          className="rounded-md border border-destructive/40 bg-background px-3 py-1.5 text-xs font-medium text-destructive shadow-sm hover:bg-destructive/10 disabled:opacity-50">
                    {removing === service.id ? 'Removing…' : 'Remove'}
                  </button>
                </div>
                <div id={confirmationId} popover="auto" className="destructive-confirmation">
                  <p><strong>Remove {serviceLabel(service).title || serviceLabel(service).type}?</strong></p>
                  <p className="hint">The import will stop. Historical entries will be kept.</p>
                  <div className="confirm-delete">
                    <button type="button" className="yes"
                            popoverTarget={confirmationId} popoverTargetAction="hide"
                            onClick={() => handleRemove(service)}>Confirm remove</button>
                    <button type="button" className="cancel"
                            popoverTarget={confirmationId} popoverTargetAction="hide">Cancel</button>
                  </div>
                </div>
              </li>
              );
            })}
          </ul>}

      {!hasTwitter &&
        <div>
          <h4 className="mb-2 text-sm font-semibold">Import</h4>
          <a href="/account/import/twitter" className={`inline-block ${primaryButtonClass}`}>
            Import Tweet
          </a>
        </div>}

      <form className="mt-6" onSubmit={handleAdd}>
        <h4 className="mb-2 text-sm font-semibold">Import RSS, Atom or JSON Feed</h4>
        <div className="flex gap-2">
          <input type="url" required value={url} onChange={(e) => setUrl(e.target.value)}
                 placeholder="https://example.com/feed.xml"
                 className="min-w-0 flex-1 rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:border-ring focus:outline-none focus:ring-1 focus:ring-ring" />
          <button type="submit" disabled={adding} className={primaryButtonClass}>
            {adding ? 'Adding…' : 'Add'}
          </button>
        </div>
      </form>
    </div>
  );
}

export function FeedImportPage() {
  const data = /** @type {Window & {feedImportData: {
   * services: Record<string, ServiceData>, states: Record<string, ServiceState>, target: string
   * }}} */ (/** @type {unknown} */ (window)).feedImportData;
  const [services, setServices] = useState(data.services ?? {});
  return <ImportPanel services={services} states={data.states ?? {}} target={data.target}
    onServicesChange={setServices} />;
}
