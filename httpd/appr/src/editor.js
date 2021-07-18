import React, { useState, useEffect, useMemo } from 'react';
import { Node } from 'slate'
import {
  SlatePlugins,
  createDeserializeHTMLPlugin,
  serializeHTMLFromNodes,
  deserializeHTMLToDocumentFragment,
  useSlatePluginsActions,
} from '@udecode/slate-plugins';


import {
    InlineToolbarElements,
    initialValueEmpty,
    defaultPlugins,
    components,
    editor,
    options,
} from './options';

const editableProps = {
    placeholder: '开始记录...',
    style: {
        padding: '15px',
        boxSizing: "border-box",
        border: "1px solid #ddd",
        cursor: "text",
        borderRadius: "2px",
        marginBottom: "1em",
        overflow: "auto",
    },
};

const serializePlainText = nodes => {
    return nodes.map(n => Node.string(n)).join('\n')
}


const OnPageEditor = ({
    id = "",
    feedId = "",
    content = "",
    postEntry
}) => {
    const eid = `{id}-editor`;
    const [value, setValue] = useState(null);
    const { resetEditor } = useSlatePluginsActions(eid);

    const plugins = useMemo(() => {
        const p = [...defaultPlugins];
        p.push(createDeserializeHTMLPlugin({ plugins: p }));
        return p;
    }, []);

    const initialValue = useMemo(() => {
        if (content) {
            try {
                // how to test content is raw?
                return JSON.parse(content);
            } catch (e) {
                // fail safe to html parse
                return deserializeHTMLToDocumentFragment(editor, {
                    plugins,
                    element: content,
                });
            }
        }
        return initialValueEmpty;
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [content]);

    useEffect(() => {
        if (value) {
          const html = serializeHTMLFromNodes(editor, {
            plugins,
            nodes: value,
          });
          console.log(html);
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
      }, [value]);    

    const onChange = (slateValue) => {
        console.log(JSON.stringify(slateValue));
        setValue(slateValue);
    };

    const onPostEntry = () => {
        if (!value) {
            return
        }
        
        const htmlBody = serializeHTMLFromNodes(editor, {
            plugins,
            nodes: value,
        });
        const rawBody = JSON.stringify(value)

        var plainText = serializePlainText(value);
        if (plainText.length < 8) {
            return;
        }

        var formData = new FormData();
        formData.set("id", id);
        formData.set("feedid", feedId);
        formData.set("body", htmlBody);
        formData.set("rawBody", rawBody);
        postEntry(formData)
            .then(() => {
                // editor.moveToRangeOfDocument().insertBlock("");
                // not working;
                resetEditor(eid);
                setValue(initialValueEmpty);
                console.log("set empty");
            }).catch(error => console.error(error));
    };

    return (
        <div className="sharebox" id="shareform">
            <InlineToolbarElements />
            <SlatePlugins
                id={eid}
                plugins={plugins}
                components={components}
                options={options}
                editableProps={editableProps}
                initialValue={initialValue}
                onChange={(newValue) => {
                    onChange(newValue);
                }}
            />
            <div className="post">
                <span className="max_info"></span>
                <input className="submit" type="submit" value="发布" onClick={onPostEntry} />
            </div>
        </div>
    );
}

export default OnPageEditor
