// @ts-check

/** @param {unknown} msg */
export function dprint(msg) {
    if (typeof window !== 'undefined' && window.console && window.console.log) {
        window.console.log(msg);
    }
}

/**
 * @template T
 * @param {string} url
 * @returns {Promise<T>}
 */
export function getJSON(url) {
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
    }).then(response => response.json()) // parses response to JSON
}

/**
 * @template T
 * @param {string} url
 * @param {Record<string, string>} data
 * @returns {Promise<T>}
 */
export function postJSON(url, data) {
    const params = new URLSearchParams();
    for (var key in data) {
        params.append(key, data[key]);
    }
    return fetch(url, {
        body: params,
        cache: 'no-cache',
        credentials: 'same-origin', // include, same-origin, *omit
        headers: {
            'user-agent': 'Mozilla/4.0 MDN',
            'Content-Type': 'application/x-www-form-urlencoded'
        },
        method: 'POST', // *GET, POST, PUT, DELETE, etc.
        mode: 'cors', // no-cors, cors, *same-origin
        redirect: 'follow', // manual, *follow, error
        referrer: 'no-referrer', // *client, no-referrer
    }).then(response => response.json()) // parses response to JSON
}

// Warning: When using FormData to submit POST requests using XMLHttpRequest or 
// the Fetch_API with the multipart/form-data Content-Type (e.g. when uploading 
// Files and Blobs to the server), do not explicitly set the Content-Type header 
// on the request. Doing so will prevent the browser from being able to set the 
// Content-Type header with the boundary expression it will use to delimit form 
// fields in the request body.
/**
 * @template T
 * @param {string} url
 * @param {FormData} formData
 * @returns {Promise<T>}
 */
export function postForm(url, formData) {
    return fetch(url, {
        body: formData,
        cache: 'no-cache',
        credentials: 'same-origin', // include, same-origin, *omit
        headers: {
            'user-agent': 'Mozilla/4.0 MDN',
        },
        method: 'POST', // *GET, POST, PUT, DELETE, etc.
        mode: 'cors', // no-cors, cors, *same-origin
        redirect: 'follow', // manual, *follow, error
        referrer: 'no-referrer', // *client, no-referrer
    }).then(response => response.json()) // parses response to JSON
}

/** intersperse: Return an array with the separator interspersed between
 * each element of the input array.
 *
 * > _([1,2,3]).intersperse(0)
 * [1,0,2,0,3]
 *
 * @template T, S
 * @param {T[]} arr
 * @param {S} sep
 * @returns {(T | S)[]}
 */
export function intersperse(arr, sep) {
    if (arr.length === 0) {
        return [];
    }

    return arr.slice(1).reduce(function (xs, x) {
        return xs.concat([sep, x]);
    }, /** @type {(T | S)[]} */ ([arr[0]]));
}
