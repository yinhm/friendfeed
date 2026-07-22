// @ts-check

import React, { useState } from 'react';


export function Search() {
    const [value, setValue] = useState("");

    return (
        <div className="section">
            <h3><label htmlFor="search-query">Search</label></h3>
            <form action="/search">
                <input id="search-query" name="q" type="search"
                    value={value}
                    onChange={(e) => setValue(e.target.value)} />
            </form>
        </div>
    );
}
