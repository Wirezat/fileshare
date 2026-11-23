import { EditorView, basicSetup } from "codemirror";
import { Compartment, EditorState } from "@codemirror/state";
import { keymap } from "@codemirror/view";
import { indentWithTab } from "@codemirror/commands";
import { LanguageDescription } from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { oneDark } from "@codemirror/theme-one-dark";

/* Creates a code editor for text in parent, colored by the language that filename points to.
   onSave runs on Ctrl+S, onChange(dirty) whenever the text starts or stops differing from the last clean state.
   @returns {{text: () => string, reset: (text: string) => void, markClean: () => void, focus: () => void, destroy: () => void}} */
export function createEditor(parent, { text, filename, dark, onSave, onChange }) {
    const language = new Compartment();
    let dirty = false;
    const setDirty = next => {
        if (next !== dirty) {
            dirty = next;
            onChange(dirty);
        }
    };
    const view = new EditorView({
        parent,
        state: EditorState.create({
            doc: text,
            extensions: [
                basicSetup,
                keymap.of([{ key: "Mod-s", preventDefault: true, run: () => { onSave(); return true; } }, indentWithTab]),
                language.of([]),
                dark ? oneDark : [],
                EditorView.theme({ "&": { height: "100%" }, ".cm-scroller": { overflow: "auto" } }),
                EditorView.updateListener.of(u => { if (u.docChanged) setDirty(true); }),
            ],
        }),
    });
    LanguageDescription.matchFilename(languages, filename)?.load()
        .then(support => view.dispatch({ effects: language.reconfigure(support) }))
        .catch(() => {});
    return {
        text: () => view.state.doc.toString(),
        reset(next) {
            view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: next } });
            setDirty(false);
        },
        markClean: () => setDirty(false),
        focus: () => view.focus(),
        destroy: () => view.destroy(),
    };
}
