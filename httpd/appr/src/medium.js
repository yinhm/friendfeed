import React, { useState, useEffect, useMemo } from 'react';
import { Node } from 'slate'
import {
  SlatePlugins,
  createDeserializeHTMLPlugin,
  serializeHTMLFromNodes,
  deserializeHTMLToDocumentFragment,
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
    const [value, setValue] = useState(null);

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
    }, [content]);

    useEffect(() => {
        if (value) {
          const html = serializeHTMLFromNodes(editor, {
            plugins,
            nodes: value,
          });
        }
      }, [value]);    

    const onChange = (slateValue) => {
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
                setValue(initialValue);
            }).catch(error => console.error(error));
    };

    return (
        <div className="sharebox" id="shareform">
            <InlineToolbarElements />
            <SlatePlugins
                id={id || "inline-editor"}
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
