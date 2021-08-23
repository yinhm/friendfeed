import React from 'react';
import {
    Cell,
    Column,
    ColumnHeaderCell,
    Table,
} from "@blueprintjs/table";


export function Tabular({columns, data}) {
    const numRows = data.length;

    console.log("rending rows, total num:", numRows);

    const getCellStyle = (data) => {
        return {
            backgroundColor: `hsl(${120 * ((120 - data) / 120) * -1 +
                120}, 100%, 67%)`,
        }
    };

    const renderCell = (rowIndex, columnIndex) => {
        const row = data[rowIndex];
        return (
            <Cell style={getCellStyle(row[columnIndex])}>
                {row[columnIndex]}
            </Cell>
        );
    };

    const renderColumnHeaderCell = (columnIndex) => {
        const columnName = columns[columnIndex];
        return <ColumnHeaderCell name={columnName} />;
    };
    
    const renderColumns = () => {
        const ret = [];

        columns.forEach(columnName => {
            ret.push(
                <Column
                    key={columnName}
                    cellRenderer={renderCell}
                    columnHeaderCellRenderer={renderColumnHeaderCell}
                />,
            );
        });

        return ret;
    }

    return (
        <Table enableRowResizing={true}
            numRows={numRows}
            defaultColumnWidth={75}>
            {renderColumns()}
        </Table>
    )
}
