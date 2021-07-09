
export function dprint(msg) {
    if (typeof window !== 'undefined' && window.console && window.console.log) {
        window.console.log(msg);
    }
}

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

/* intersperse: Return an array with the separator interspersed between
 * each element of the input array.
 *
 * > _([1,2,3]).intersperse(0)
 * [1,0,2,0,3]
 */
export function intersperse(arr, sep) {
    if (arr.length === 0) {
        return [];
    }

    return arr.slice(1).reduce(function (xs, x, i) {
        return xs.concat([sep, x]);
    }, [arr[0]]);
}
