# Project instructions

## Frontend UI work

Before changing UI in `web/src`, read `web/src/components/ui/README.md` and inspect the existing components in `web/src/components/ui/`.

Use an existing UI atom for its intended role before writing a new control or surface:

- Use `Button` for actions, including form submission and in-app navigation handled by code.
- Use `LinkButton` for navigation with a real URL.
- Use `Card` for reusable card surfaces. Keep ordinary layout containers as semantic HTML.

If an atom almost fits, extend its props or variants in the atom and its shared styles before adding a page-specific duplicate. If a new primitive will be reused, create it in `web/src/components/ui/` first, then use it in the feature. Keep page styles for layout and context, and keep shared visual rules in the atom or `web/src/app.css`.

When adding or changing an atom, update `web/src/components/ui/README.md` with its purpose and public options. Preserve HTML semantics and accessibility: actions are buttons, URL navigation is a link, and disabled/loading states remain clear.
