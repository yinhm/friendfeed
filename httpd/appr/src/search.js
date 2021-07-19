import React, { useState } from 'react';


export function Search(props) {
    const [value, setValue] = useState("");

    // const doQuery = (event) => {
    //     var value = event.target.value;
    // }

    return (
        <div class="section">
            <h3>Search</h3>
            <form action="/search">
                <input name="q" type="search"
                    value={value}
                    onChange={(e) => setValue(e.target.value)} />
            </form>
        </div>
    );
}
