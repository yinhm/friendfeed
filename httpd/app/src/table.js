import React from 'react';
import { useTable } from 'react-table'


export function Tabular({columns, data, getCellProps}) {

    // WARN: make sure pass columns and data from parent.
    //
    // Maximum update depth exceeded error even using React.useMemo
    // TLDR
    // https://github.com/tannerlinsley/react-table/issues/2369

    // const rawdata = JSON.parse(rawBody);
    // console.log(rawdata);

    // const data = React.useMemo(() => rawdata.data, [rawdata.data])

    // const columns = React.useMemo(() => {
    //     return rawdata.columns.map(function (column, index) {
    //         return {
    //             id: "header" + index,
    //             Header: column,
    //             accessor: (row, idx) => {
    //                 return row[index]
    //             }
    //         };
    //     });
    // }, [rawdata.columns])

    const {
        getTableProps,
        getTableBodyProps,
        headerGroups,
        rows,
        prepareRow,
    } = useTable({ columns, data })

    return (
        <table {...getTableProps()} style={{ border: 'solid 1px blue' }}>
            <thead>
                {headerGroups.map(headerGroup => (
                    <tr {...headerGroup.getHeaderGroupProps()}>
                        {headerGroup.headers.map(column => (
                            <th
                                {...column.getHeaderProps()}
                                style={{
                                    borderBottom: 'solid 3px red',
                                    background: 'aliceblue',
                                    color: 'black',
                                    fontWeight: 'bold',
                                }}
                            >
                                {column.render('Header')}
                            </th>
                        ))}
                    </tr>
                ))}
            </thead>
            <tbody {...getTableBodyProps()}>
                {rows.map(row => {
                    prepareRow(row)
                    return (
                        <tr {...row.getRowProps()}>
                            {row.cells.map(cell => {
                                return (
                                    <td
                                        // Return an array of prop objects and react-table will merge them appropriately
                                        {...cell.getCellProps([
                                            {
                                                className: cell.column.className,
                                                style: cell.column.style,
                                            },
                                            getCellProps(cell),
                                        ])}
                                    >
                                        {cell.render('Cell')}
                                    </td>
                                )
                            })}
                        </tr>
                    )
                })}
            </tbody>
        </table>
    )
}
