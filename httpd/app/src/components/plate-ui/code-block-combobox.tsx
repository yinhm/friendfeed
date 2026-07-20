'use client';

import React, { useEffect, useState } from 'react';
import { cn } from '@udecode/cn';
import { useEditorReadOnly, useEditorRef, useElement } from 'platejs/react';

import { Icons } from 'components/icons';

import { Button } from './button';
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from './command';
import { Popover, PopoverContent, PopoverTrigger } from './popover';

// Plate 36 moved this UI-owned list out of the headless code-block package.
// Keep the values from the previous component so stored `lang` properties and
// the language picker remain compatible.
const codeBlockLanguages = {
  antlr4: 'ANTLR4',
  bash: 'Bash',
  c: 'C',
  csharp: 'C#',
  css: 'CSS',
  coffeescript: 'CoffeeScript',
  cmake: 'CMake',
  dart: 'Dart',
  django: 'Django',
  docker: 'Docker',
  ejs: 'EJS',
  erlang: 'Erlang',
  git: 'Git',
  go: 'Go',
  graphql: 'GraphQL',
  groovy: 'Groovy',
  html: 'HTML',
  java: 'Java',
  javascript: 'JavaScript',
  json: 'JSON',
  jsx: 'JSX',
  kotlin: 'Kotlin',
  latex: 'LaTeX',
  less: 'Less',
  lua: 'Lua',
  makefile: 'Makefile',
  markdown: 'Markdown',
  matlab: 'MATLAB',
  markup: 'Markup',
  objectivec: 'Objective-C',
  perl: 'Perl',
  php: 'PHP',
  powershell: 'PowerShell',
  properties: '.properties',
  protobuf: 'Protocol Buffers',
  python: 'Python',
  r: 'R',
  ruby: 'Ruby',
  sass: 'Sass (Sass)',
  scss: 'Sass (Scss)',
  scheme: 'Scheme',
  sql: 'SQL',
  shell: 'Shell',
  swift: 'Swift',
  svg: 'SVG',
  tsx: 'TSX',
  typescript: 'TypeScript',
  wasm: 'WebAssembly',
  yaml: 'YAML',
  xml: 'XML',
} as const;

const languages: { value: string; label: string }[] = [
  { value: 'text', label: 'Plain Text' },
  ...Object.entries(codeBlockLanguages).map(([key, val]) => ({
    value: key,
    label: val as string,
  })),
];

export function CodeBlockCombobox() {
  const editor = useEditorRef();
  const element = useElement();
  const readOnly = useEditorReadOnly();
  const [value, setValue] = useState(element.lang ?? 'text');
  const [open, setOpen] = useState(false);

  useEffect(() => setValue(element.lang ?? 'text'), [element.lang]);

  if (readOnly) return null;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          role="combobox"
          aria-expanded={open}
          className="h-5 justify-between px-1 text-xs"
          size="xs"
        >
          {value
            ? languages.find((language) => language.value === value)
                ?.label
            : 'Plain Text'}
          <Icons.chevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-0">
        <Command>
          <CommandInput placeholder="Search language..." />
          <CommandEmpty>No language found.</CommandEmpty>

          <CommandList>
            {languages.map((language) => (
              <CommandItem
                key={language.value}
                value={language.value}
                className="cursor-pointer"
                onSelect={(_value) => {
                  const path = editor.api.findPath(element);
                  if (path) editor.tf.setNodes({ lang: _value }, { at: path });
                  setValue(_value);
                  setOpen(false);
                }}
              >
                <Icons.check
                  className={cn(
                    'mr-2 h-4 w-4',
                    value === language.value ? 'opacity-100' : 'opacity-0'
                  )}
                />
                {language.label}
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
