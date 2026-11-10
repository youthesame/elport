# Same-name uploads get their id in the local name

eLabFTW names every image pasted into the editor `image.png`, so an ordinary note holds several
uploads with the same `real_name` and different content. pull, fetch, merge, and `view -o` refused
such an entity ("attachment basename collision with different content") and wrote no attachment at
all, which left an agent reading the note no way forward except calling the API itself.

- **Each upload gets its own local name.** Uploads are ordered by id; the first keeps its safe
  `real_name`, and a later one whose name matches regardless of case gets its id before the
  extension (`image.png`, `image-1235.png`). Names are given once, when the upload list is read, and
  every command uses them. A new upload never renames an existing one, as its id is larger. Deleting
  the first upload of a name on the server moves that name to the next one.
- **push matches a local file against uploads with either name**, the local one or the upload's own,
  and the hash picks among them. A file pull named `image-1235.png` is that upload, and a local
  `image.png` rewritten and pushed matches the newer upload of that name instead of uploading again.
  The name still has to match, so two identical files with different names keep their own uploads
  and links. An upload without a hash still matches by name and size.
- Identical uploads of the same name are no longer merged into one file. Each is written under its
  own name.

## Considered options

- **Number the later uploads (`image-2.png`).** Deleting any earlier upload would rename every
  later file.
- **Name every same-name upload after its id, the first included.** Pasting a second image would
  rename the first file already in the working copy.
