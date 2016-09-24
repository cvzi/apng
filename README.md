# apng
Creates an apng animation file from mutiple png images.

This was running on Windows 10 64bit with Go 1.7

To run it, just type `make` or 

`apng.exe -d $delays -i $frames -o $out`
 - `$delays` is a text file containing the display duration for each frame. 
 - `$frames` is a folder containg all the frames i.e. png images. 
 - `$out` is the output apng file.

This is my first go program ever. It was not tested and there are probably a lot of bugs. It's just a project to learn the language a little bit.

The resulting apng files are not recompressed - the individual png files are just copied as they are - which is not ideal at all.