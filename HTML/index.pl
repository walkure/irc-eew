#!/usr/bin/env perl

# show list of saved EEW data
# walkure at kmc.gr.jp

use strict;
use warnings;
use CGI::Carp qw(fatalsToBrowser);

use Encode;
use Earthquake::EEW::Decoder;
use CGI;

my $ddir = '../eewlog/';
my $ppd = 60;

my $q = new CGI;
my $page = $q->param('page');
if(defined $page){
	$page += 0;
}else{
	$page = 0;
}

print << "_HTML_";
Content-Type:text/html;charset=euc-jp

<html><head><title>index of EEW Data</title></head>
<body>
[<a href="../">戻ル</a>]
	<ul>
_HTML_

opendir(my $dh,$ddir) or die "opendir($ddir):$!";
my $file;
#while($file = readdir($dh)){
my @files = reverse sort readdir($dh);
foreach my $fid($page*$ppd .. ($page+1)*$ppd){
	my $file = $files[$fid];
	last unless defined $file;
	next if $file =~/^\./;

	my $path = $ddir.'/'.$file;
	open(my $fh,$path) or die "Cannot open $path:$!";
	my $len = sysread $fh, my($buf), 1024;
	close($fh);
	my $msg = get_eew_summary($buf);
	print qq|<li><a href="./eew-show.pl?data=$file">$msg</a></li>\n|;

}

print "</ul><hr>\n";

foreach my $p(0 .. (scalar @files)/$ppd-1){
	if($p eq $page){
		printf qq|[<a href="?page=$p">$p</a>] |;
	}else{
		printf qq|<a href="?page=$p">$p</a> |;
	}
}

print << "_HTML_";
</body></html>
_HTML_

sub get_eew_summary
{
	my $buf = shift;
	my $eew = Earthquake::EEW::Decoder->new();

	my $d = $eew->read_data($buf);
	conv_charset($d);

	my @wd = $d->{warn_time} =~ /\d\d/og;
	my @ed = $d->{eq_time}   =~ /\d\d/og;
	
	my $warnmsg = "20$wd[0]/$wd[1]/$wd[2] $wd[3]:$wd[4]:$wd[5]";
	my $eqedmsg = "$ed[3]:$ed[4]:$ed[5]";

	my $times = '第'.sprintf '%02d報%s',
		$d->{warn_num},
		$d->{NCN_type} >=8 ? '(最終)' : '&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;';

	my $msg;
	if($d->{'msg_type_code'} == 10){
        $msg = $warnmsg.$times.' ('.$eqedmsg.'発生) 取り消し';
	}else{
        my $center = sprintf('震央:N%.01f/E%0.01f(%s)深さ%dkm',$d->{'center_lat'},$d->{'center_lng'},$d->{'center_name'},$d->{'center_depth'});
        my $magnitude = sprintf(' 最大:M%.01f 震度%s',$d->{'magnitude'},$d->{'shindo'});
        $msg = $warnmsg.$times.' ('.$eqedmsg.'発生)'.$center.$magnitude;
	}
	
	$msg.= ' (地域予想震度情報あり)' if defined $d->{EBI};

	$msg;
}

sub conv_charset
{
	my $d = shift;
	foreach my $key(keys %$d){
		$d->{$key} = Encode::encode('euc-jp',$d->{$key});
	}
}
