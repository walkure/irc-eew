#!/usr/bin/env perl

# show list of saved EEW data
# walkure at kmc.gr.jp

use strict;
use warnings;
#use CGI::Carp qw(fatalsToBrowser);
use File::Slurp qw(read_file);
use CGI;
use utf8;
use lib '../lib/';
use Earthquake::EEW::Decoder;

my $ddir = '../eewlog/';
my $path_base = './';

binmode STDOUT, ":utf8";
my $query = new CGI;

main($query);

sub main
{
	my $q = shift;
	my $year = $q->param('year') ;
	my $month = $q->param('month');
	my $day = $q->param('day');

	$year = saturate(defined $year ? $year + 0 : 0 , 2000,3000);
	$month = saturate(defined $month ? $month + 0 : 0,1,12);
	$day = saturate(defined $day ? $day + 0 : 0 , 1,31);

	print << "_HTML_";
Content-Type:text/html;charset=utf-8

<html><head><title>list of EEW Data</title></head>
<body>
	<ul>
_HTML_

	unless( -e "$ddir/$year" ){
		opendir(my $dh,$ddir) or die "opendir($ddir):$!";
		foreach my $d (sort readdir($dh)){
			next unless $d =~ /^\d+/;
			print qq|<li><a href="$path_base?year=$d">${d}年</a></li>\n|;
		}
		closedir($dh);
		print << "_HTML_";
</body></html>
_HTML_
		return;
	}
	unless( -e sprintf('%s/%04d/%02d',$ddir,$year,$month)){
		opendir(my $dh,"$ddir/$year") or die "opendir($ddir):$!";
		foreach my $d (sort readdir($dh)){
			next unless $d =~ /^\d+/;
			$d += 0;
			print qq|<li><a href="$path_base?year=$year&month=$d">${d}月</a></li>\n|;
		}
		closedir($dh);
		print << "_HTML_";
</body></html>
_HTML_
		return;
	}
	unless( -e sprintf('%s/%04d/%02d/%02d',$ddir,$year,$month,$day)){
		opendir(my $dh,sprintf('%s/%04d/%02d',$ddir,$year,$month)) or die "opendir($ddir):$!";
		foreach my $d (sort readdir($dh)){
			next unless $d =~ /^\d+/;
			$d += 0;
			print qq|<li><a href="$path_base?year=$year&month=$month&day=$d">${d}日</a></li>\n|;
		}
		closedir($dh);
		print << "_HTML_";
</body></html>
_HTML_
		return;
	}

	opendir(my $dh,sprintf('%s/%04d/%02d/%02d',$ddir,$year,$month,$day)) or die "opendir($ddir):$!";
	foreach my $d (sort readdir($dh)){
		next unless $d =~ /^\d+\.\d+/;

		my $path = sprintf('%s/%04d/%02d/%02d/%s',$ddir,$year,$month,$day,$d);
		my $summary = get_eew_summary($path);
		print qq|<li><a href="${path_base}eew-view.pl?name=$d">$summary</a></li>\n|;
	}
	closedir($dh);
	print << "_HTML_";
</body></html>
_HTML_

}
sub saturate{
	my($value,$min,$max) = @_;

	return $max if $value > $max;
	return $min if $value < $min;
	$value;
}

sub get_eew_summary
{
	my $path = shift;
	my $eew = Earthquake::EEW::Decoder->new();
	my $data = read_file($path);
	my $d = $eew->read_data($data);

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
