// @ts-check

import React, {useState} from 'react';
import {getJSON} from './utils';

/**
 * @typedef {object} ServiceData
 * @property {string} id
 * @property {string} name
 * @property {string} [icon]
 * @property {string} [profile]
 * @property {string} [username]
 */

/**
 * Connected import services with removal, plus entry points for adding
 * new imports (e.g. the Twitter OAuth flow).
 *
 * The services map is owned by the parent (AccountApp) so deletions
 * survive tab switches; this panel only renders and reports changes.
 *
 * @param {{services: Record<string, ServiceData>,
 * onServicesChange?: (services: Record<string, ServiceData>) => void}} props
 */
export function ImportPanel(props) {
  const [removing, setRemoving] = useState(/** @type {string | null} */ (null));
  const [error, setError] = useState(/** @type {string | null} */ (null));

  const services = props.services ?? {};
  const list = Object.values(services);
  const hasTwitter = 'twitter' in services;

  /** @param {ServiceData} service */
  const handleRemove = (service) => {
    if (!window.confirm(`Remove ${service.id}?`)) {
      return;
    }
    setError(null);
    setRemoving(service.id);
    getJSON(`/account/service/${service.id}/delete`)
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

  return (
    <div className="max-w-xl">
      <h3 className="mb-4 text-lg font-semibold">Import Services</h3>

      {error &&
        <div role="alert" className="mb-4 rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>}

      {list.length === 0
        ? <div className="mb-4 text-sm text-gray-500">No services connected yet.</div>
        : <ul className="mb-4 divide-y divide-gray-200 rounded-md border border-gray-200">
            {list.map((service) => (
              <li key={service.id} className="flex items-center justify-between gap-3 px-3 py-2">
                <div className="text-sm">
                  <span className="font-medium">{service.name}</span>
                  {service.username &&
                    <span className="ml-2 text-gray-500">
                      {service.profile
                        ? <a href={service.profile} className="underline">{service.username}</a>
                        : service.username}
                    </span>}
                </div>
                <button type="button"
                        disabled={removing === service.id}
                        onClick={() => handleRemove(service)}
                        className="rounded-md border border-gray-300 px-2 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-50">
                  {removing === service.id ? 'Removing…' : `remove ${service.id}`}
                </button>
              </li>
            ))}
          </ul>}

      {!hasTwitter &&
        <div>
          <h4 className="mb-2 text-sm font-semibold">Import</h4>
          <a href="/account/import/twitter"
             className="inline-block rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">
            Import Tweet
          </a>
        </div>}
    </div>
  );
}
