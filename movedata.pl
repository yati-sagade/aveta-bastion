use strict;
use warnings;
use feature qw(say);
use File::Path qw(make_path);
use File::Copy qw(move);
use File::Spec;
use File::HomeDir qw(my_home);
use Data::Dumper;

# Given a tagname, moves all current subdirectories of $aveta_training_home to
# "${aveta_training_home}-final/${tagname}", creating the directories as needed.

my $home                = my_home();
my $aveta_training_home = File::Spec->catfile(my_home, "aveta-training-data");

my $tagname             = shift @ARGV // die "Need tagname.";
my $outdir              = "${aveta_training_home}-final/${tagname}";

if (! -d $aveta_training_home) {
    say "$aveta_training_home does not exist. Nothing to do.";
    exit 0;
}

# Destination directories are numbered 0, 1, ... Get the next number we
# can use.
my $start_index = 0;
if (! -d $outdir) {
    make_path($outdir) or die $!;
} else {
    my @existing_entries = get_entries($outdir);
    if (@existing_entries) {
        $existing_entries[-1] =~ /(\d+)$/;
        $start_index = $1 + 1;
    }
}

my @entries = get_entries($aveta_training_home);
while (my ($idx, $entry) = each @entries) {
    my $outfile              = File::Spec->catfile($outdir, $idx+$start_index);
    my (undef, undef, $filename) = File::Spec->splitpath($entry);
    move($entry, $outfile);
    write_orig_name($outfile, $filename);
}

sub write_orig_name {
    my ($outfile, $name) = @_;
    open my $fh, ">:encoding(utf8)", File::Spec->catfile($outfile, "original-name")
        or die $!;
    print $fh $name;
    close $fh;
}

sub get_entries {
    my $dirname = shift // die "Need directory path";
    opendir my $dh, $dirname or die $!;
    my @entries = grep { -d $_ }
                  map  { File::Spec->catfile($dirname, $_) } 
                  sort
                  grep { !/^\./ } readdir $dh;
    closedir $dh;
    return @entries;
}

__END__

=pod

=head1 NAME

    movedata.pl

=head1 SYNOPSIS

    perl movedata.pl tagname

=head1 DESCRIPTION

    Move directories in ~/aveta-training-data/ to
     ~/aveta-training-data-final/${tagname}. This script is meant to be used
    with aveta-bastion (github.com/yati-sagade/aveta-bastion) to move training
    data for aveta under categories (simple track, 8 shaped track etc).
    
    Each driving session using aveta-bastion results in a timestamped directory
    being created under ~/aveta-training-data. Once multiple drives for
    a particular track config/environment are captured, this script can be
    used to move everything under ~/aveta-training-data to
    ~/aveta-training-data-final/${tagname}.

=head1 AUTHORS

    Yati Sagade "<yati.sagade@gmail.com>"

=cut

